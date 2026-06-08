"""GUI-приложение: окно настроек + трей + дисплейный цикл в фоне.

Запуск: `python -u app.py [--autostart]`
  --autostart    запустить сразу свёрнутым в трей (для автозапуска)
  --no-display   не открывать COM-порт (только preview)
  --display-only только дисплей, без preview
"""
from __future__ import annotations
import argparse
import os
import sys
import time
import signal
import threading
from pathlib import Path

_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))
# на случай запуска из другой папки — добавим и корень проекта
_ROOT = _HERE
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from core.config import Config, default_config_path
from core.logger import get_logger
from sensors.hardware import HardwareMonitor


# -----------------------------------------------------------------------
# Display loop, вынесенный в отдельную функцию для запуска в потоке
# -----------------------------------------------------------------------
def _display_loop(cfg: Config, stop: dict, log, args):
    """Главный цикл рендера+отправки. Совместим с main.py."""
    from themes.neon_gengar import NeonGengarTheme
    from themes.gengar import GengarTheme
    from themes.minimal import MinimalTheme
    from themes.cyberpunk import CyberpunkTheme
    from display.serial_lcd import SerialLCD

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
        try:
            from display.preview import PreviewWindow
            displays.append(PreviewWindow(w, h, cfg.ui.preview_scale))
        except Exception as e:
            log.warning("preview init failed: %s", e)

    if not args.no_display:
        try:
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
        except Exception as e:
            log.error("display init failed: %s", e)
    if not displays:
        log.error("no display available")
        stop["flag"] = True
        return

    # --- монитор и тема ---
    u = cfg.user
    hw = HardwareMonitor(
        extra_disks=u.disks,
        opencode_url=u.opencode_url,
        opencode_cookie=u.opencode_cookie,
        opencode_token=u.opencode_token,
        network_type=u.network_type,
        ethernet_iface=u.ethernet_iface,
    )
    # собираем тему по имени
    name = (cfg.ui.theme or "neon_gengar").lower()
    if name == "neon_gengar":
        theme = NeonGengarTheme(
            w, h, cfg.display.orientation,
            gif_dir=str(_HERE / "gif"),
            disks=u.disks,
            network_type=u.network_type,
        )
    elif name == "gengar":
        theme = GengarTheme(w, h, cfg.display.orientation)
    elif name == "minimal":
        theme = MinimalTheme(w, h, cfg.display.orientation)
    elif name == "cyberpunk":
        theme = CyberpunkTheme(w, h, cfg.display.orientation)
    else:
        theme = NeonGengarTheme(
            w, h, cfg.display.orientation,
            gif_dir=str(_HERE / "gif"),
            disks=u.disks, network_type=u.network_type,
        )

    fps = max(1, cfg.ui.framerate)
    period = 1.0 / fps
    log.info("display loop @ %d FPS (target)", fps)
    t0 = time.time()
    frames = 0

    partial_displays = [d for d in displays if hasattr(d, "send_region")]
    full_displays    = [d for d in displays if not hasattr(d, "send_region")]
    full_frame_pending = bool(partial_displays)

    try:
        while not stop["flag"]:
            ts = time.time()
            snap = hw.snapshot()
            img = theme.render(snap)
            for d in full_displays:
                d.send_frame(img)
            if full_frame_pending:
                for d in partial_displays:
                    d.send_frame(img)
                full_frame_pending = False
            elif partial_displays and hasattr(theme, "dirty_regions"):
                # полный кадр каждые 30 кадров — resync дисплея
                if frames % 30 == 0:
                    for d in partial_displays:
                        d.send_frame(img)
                else:
                    regions = theme.dirty_regions(snap)
                    # максимум 2 региона за кадр — ST7796 overload protection
                    for r in regions[:2]:
                        _, _key, x, y, rw, rh = r
                        for d in partial_displays:
                            d.send_region(img, x, y, rw, rh)
                        if hasattr(theme, "mark_sent"):
                            theme.mark_sent(r)
                        time.sleep(0.015)
            elif partial_displays:
                for d in partial_displays:
                    d.send_frame(img)
            frames += 1
            dt = time.time() - ts
            sleep = period - dt
            if sleep > 0:
                time.sleep(sleep)
    except Exception as e:
        log.error("display loop crashed: %s", e)
    finally:
        log.info("display loop stopped after %d frames / %.1fs",
                 frames, time.time() - t0)
        for d in displays:
            try:
                d.close()
            except Exception:
                pass


# -----------------------------------------------------------------------
# App: GUI + Tray + display loop
# -----------------------------------------------------------------------
class App:
    def __init__(self, args):
        self.args = args
        self.cfg = Config.load(args.config)
        # подтянем актуальные настройки opencode из файлов (если cookie/token
        # в GUI пустые — берём из opencode_*.txt)
        cookie_file = _HERE / "opencode_cookie.txt"
        token_file = _HERE / "opencode_token.txt"
        if not self.cfg.user.opencode_cookie and cookie_file.exists():
            self.cfg.user.opencode_cookie = cookie_file.read_text(
                "utf-8", "ignore").strip()
        if not self.cfg.user.opencode_token and token_file.exists():
            self.cfg.user.opencode_token = token_file.read_text(
                "utf-8", "ignore").strip()
        self.log = get_logger("app", self.cfg.log_level)
        self.stop = {"flag": False}
        self.display_thread: threading.Thread | None = None

        from gui import ConfigGUI
        from tray import Tray
        self.gui = ConfigGUI(
            self.cfg,
            on_start=self._on_start,
            on_tray=self._on_tray,
        )
        self.tray = Tray(
            on_show=self._on_tray_show,
            on_quit=self._on_quit,
        )

    # --- display control ---
    def _on_start(self, cfg: Config):
        """Callback из GUI: 'Запустить экран'."""
        self.stop["flag"] = True
        if self.display_thread and self.display_thread.is_alive():
            self.display_thread.join(timeout=3)
        self.stop = {"flag": False}
        self.display_thread = threading.Thread(
            target=_display_loop,
            args=(cfg, self.stop, self.log, self.args),
            daemon=True,
        )
        self.display_thread.start()

    def _stop_display(self):
        self.stop["flag"] = True

    # --- tray/close handling ---
    def _on_tray(self):
        """Из GUI: 'В трей' — сворачиваем окно, запускаем иконку."""
        if not self.tray.start():
            self.log.warning("pystray unavailable — окно просто скрыто")
        self.gui.hide()

    def _on_tray_show(self):
        """Из трея: 'Показать окно'."""
        self.gui.show()

    def _on_quit(self):
        """Из трея: 'Выход' — полное завершение."""
        self._stop_display()
        try:
            self.tray.stop()
        except Exception:
            pass
        try:
            self.gui.root.quit()
        except Exception:
            pass
        os._exit(0)

    def run(self):
        # сигналы — мягкое завершение
        def _sig(*_):
            self._on_quit()
        try:
            signal.signal(signal.SIGINT, _sig)
            signal.signal(signal.SIGTERM, _sig)
        except Exception:
            pass

        if self.args.autostart:
            # стартуем сразу в трей
            self._on_tray()
        else:
            self.gui.show()

        # поднимаем трей сразу — иконка всегда в трее, даже когда окно видно
        self.tray.start()

        self.gui.mainloop()


def main():
    ap = argparse.ArgumentParser(description="UsbDisplay GUI")
    ap.add_argument("--config", type=Path, default=default_config_path())
    ap.add_argument("--port", type=str, default=None)
    ap.add_argument("--preview", action="store_true")
    ap.add_argument("--no-preview", action="store_true")
    ap.add_argument("--no-display", action="store_true")
    ap.add_argument("--display-only", action="store_true")
    ap.add_argument("--orient", type=int, default=None, choices=[0, 1, 2, 3])
    ap.add_argument("--rotate", type=int, default=None)
    ap.add_argument("--brightness", type=int, default=None)
    ap.add_argument("--reset", action="store_true")
    ap.add_argument("--autostart", action="store_true",
                    help="запустить сразу свёрнутым в трей")
    args = ap.parse_args()

    app = App(args)
    app.run()
    return 0


if __name__ == "__main__":
    sys.exit(main())
