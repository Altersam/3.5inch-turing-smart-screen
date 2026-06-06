"""Точка входа. Запускает мониторинг, рендер и отправку кадров на дисплей."""
from __future__ import annotations
import argparse
import time
import signal
import sys
from pathlib import Path

from core.config import Config, default_config_path
from core.logger import get_logger
from sensors.hardware import HardwareMonitor


def get_theme(name: str, w: int, h: int, orientation: str):
    n = name.lower()
    if n == "gengar":
        from themes.gengar import GengarTheme
        return GengarTheme(w, h, orientation)
    if n == "neon_gengar":
        from themes.neon_gengar import NeonGengarTheme
        return NeonGengarTheme(w, h, orientation, gif_dir=str(Path(__file__).parent / "gif"))
    if n == "minimal":
        from themes.minimal import MinimalTheme
        return MinimalTheme(w, h, orientation)
    if n == "cyberpunk":
        from themes.cyberpunk import CyberpunkTheme
        return CyberpunkTheme(w, h, orientation)
    from themes.gengar import GengarTheme
    return GengarTheme(w, h, orientation)


def main():
    ap = argparse.ArgumentParser(description="UsbDisplay — параметры ПК на 3.5\"")
    ap.add_argument("--config", type=Path, default=default_config_path())
    ap.add_argument("--port", type=str, default=None,
                    help="COM-порт дисплея (например COM7). Иначе из конфига/AUTO")
    ap.add_argument("--preview", action="store_true",
                    help="всегда показывать preview-окно")
    ap.add_argument("--no-preview", action="store_true",
                    help="отключить preview-окно")
    ap.add_argument("--theme", type=str, default=None)
    ap.add_argument("--no-display", action="store_true",
                    help="не открывать COM-порт дисплея (только preview)")
    ap.add_argument("--display-only", action="store_true",
                    help="только дисплей, без preview-окна")
    ap.add_argument("--orient", type=int, default=None, choices=[0, 1, 2, 3],
                    help="ориентация дисплея (0=PORTRAIT, 2=LANDSCAPE)")
    ap.add_argument("--rotate", type=int, default=None,
                    help="доп. поворот картинки (0/90/180/270)")
    ap.add_argument("--brightness", type=int, default=None,
                    help="яркость 0..100 (по умолчанию 100)")
    ap.add_argument("--reset", action="store_true",
                    help="перед стартом сделать software reset дисплея")
    ap.add_argument("--clear", action="store_true",
                    help="очистить экран и выйти")
    ap.add_argument("--no-autoreset", action="store_true",
                    help="отключить авто-reset при зависании дисплея")
    args = ap.parse_args()

    cfg = Config.load(args.config)
    if args.port:
        cfg.display.port = args.port
    if args.theme:
        cfg.ui.theme = args.theme
    if args.brightness is not None:
        cfg.display.brightness = max(0, min(100, args.brightness))
    log = get_logger("main", cfg.log_level)
    log.info("UsbDisplay v0.1 — theme=%s, port=%s",
             cfg.ui.theme, cfg.display.port)

    w, h = (cfg.display.height, cfg.display.width) \
        if cfg.display.orientation == "portrait" \
        else (cfg.display.width, cfg.display.height)

    displays = []
    use_preview = cfg.ui.preview_window
    if args.preview:
        use_preview = True
    if args.no_preview:
        use_preview = False
    if args.display_only:
        use_preview = False

    if use_preview:
        from display.preview import PreviewWindow
        pv = PreviewWindow(w, h, cfg.ui.preview_scale)
        displays.append(pv)
        log.info("preview window enabled")

    if not args.no_display:
        # опционально: сброс дисплея перед запуском
        if args.reset:
            log.info("=== запрошен сброс дисплея ===")
            from display.serial_lcd import SerialLCD as _SL
            _probe = _SL(port=cfg.display.port, auto_port=True)
            if _probe.ser:
                _probe.reset()
                _probe.close()
        try:
            from display.serial_lcd import SerialLCD
            sl = SerialLCD(
                port=cfg.display.port,
                baudrate=cfg.display.baudrate,
                width=w, height=h,
                orientation=cfg.display.orientation,
                brightness=cfg.display.brightness,
                auto_port=True,
                force_orient=args.orient,
                force_rotate=args.rotate or 0,
            )
            if args.reset:
                log.info("user requested --reset, doing software reset")
                sl.reset()
            if sl.ser:
                displays.append(sl)
                log.info("serial display enabled on %s", sl.port)
                # опционально: очистить экран и выйти
                if args.clear:
                    sl.clear()
                    sl.close()
                    log.info("display cleared — exiting")
                    return 0
            else:
                log.warning("serial display not available; running preview only")
        except Exception as e:
            log.error("display init failed: %s", e)
    else:
        log.info("display disabled by flag")

    if not displays:
        log.error("no display available, exiting")
        return 1

    hw = HardwareMonitor()
    theme = get_theme(cfg.ui.theme, w, h, cfg.display.orientation)

    stop = {"flag": False}
    autoreset_enabled = not args.no_autoreset
    def on_sig(*_):
        stop["flag"] = True
    signal.signal(signal.SIGINT, on_sig)
    signal.signal(signal.SIGTERM, on_sig)

    # опционально: интерактивные команды (r=reset, c=clear) в stdin-потоке
    import threading as _thr
    if sys.stdin and sys.stdin.isatty():
        def _stdin_loop():
            while not stop["flag"]:
                try:
                    line = input()
                except EOFError:
                    break
                except Exception:
                    break
                cmd = line.strip().lower()
                if cmd == "r":
                    log.info("stdin: reset")
                    for d in displays:
                        if hasattr(d, "reset"):
                            d.reset()
                elif cmd == "c":
                    log.info("stdin: clear")
                    for d in displays:
                        if hasattr(d, "clear"):
                            d.clear()
        _thr.Thread(target=_stdin_loop, daemon=True).start()

    fps = max(1, cfg.ui.framerate)
    period = 1.0 / fps
    log.info("running @ %d FPS", fps)
    t0 = time.time()
    frames = 0
    # разделяем дисплеи: те, что умеют partial updates, и те, что нет
    partial_displays = [d for d in displays if hasattr(d, "send_region")]
    full_displays    = [d for d in displays if not hasattr(d, "send_region")]
    if partial_displays:
        log.info("partial updates enabled for %d display(s), max 1 region/tick",
                 len(partial_displays))
    # первый кадр — ПОЛНЫЙ, чтобы очистить возможный мусор в буфере дисплея
    # (например, после попыток на высоком baud)
    full_frame_pending = bool(partial_displays)
    try:
        while not stop["flag"]:
            ts = time.time()
            snap = hw.snapshot()
            img = theme.render(snap)
            # full-frame дисплеи (preview) — целиком
            for d in full_displays:
                d.send_frame(img)
            if full_frame_pending:
                # отправляем целиком на все partial-дисплеи
                log.info("sending initial full frame to clear display buffer")
                for d in partial_displays:
                    d.send_frame(img)
                full_frame_pending = False
            elif partial_displays and hasattr(theme, "dirty_regions"):
                regions = theme.dirty_regions(snap)
                # шлём ВСЕ грязные регионы за тик: 1 GIF (round-robin) + статы
                # иначе статы голодают, пока GIF-кадры меняются на 12 FPS
                for r in regions:
                    if r[0] == "gif":
                        _, _key, x, y, rw, rh = r
                    else:
                        _, _key, x, y, rw, rh = r
                    for d in partial_displays:
                        d.send_region(img, x, y, rw, rh)
                    if hasattr(theme, "mark_sent"):
                        theme.mark_sent(r)
            elif partial_displays:
                # нет dirty_regions — fallback на полный кадр
                for d in partial_displays:
                    d.send_frame(img)
            frames += 1
            dt = time.time() - ts
            sleep = period - dt
            if sleep > 0:
                time.sleep(sleep)
    except KeyboardInterrupt:
        pass
    finally:
        log.info("shutting down, sent %d frames in %.1fs",
                 frames, time.time() - t0)
        for d in displays:
            try:
                d.close()
            except Exception:
                pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
