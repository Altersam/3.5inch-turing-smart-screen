"""UsbDisplay Personal — упрощённая версия с треем и одной кнопкой «Запустить»."""
from __future__ import annotations
import os
import sys
import time
import signal
import threading
import tkinter as tk
from pathlib import Path

_HERE = Path(__file__).resolve().parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

_ICON_DIR = Path(sys._MEIPASS) if getattr(sys, 'frozen', False) else _HERE

from core.config import Config
from core.logger import get_logger
from sensors.hardware import HardwareMonitor
from tray import Tray
import autostart

PERSONAL_COOKIE = ""   # вставь свой cookie auth=
PERSONAL_TOKEN = ""    # вставь свой bearer token
PERSONAL_URL = ""      # вставь свой URL workspace


def _display_loop(cfg: Config, stop: dict, log):
    from themes.neon_gengar import NeonGengarTheme
    from display.serial_lcd import SerialLCD

    w, h = (cfg.display.height, cfg.display.width) \
        if cfg.display.orientation == "portrait" \
        else (cfg.display.width, cfg.display.height)

    displays = []
    try:
        sl = SerialLCD(
            port=cfg.display.port,
            baudrate=cfg.display.baudrate,
            width=w, height=h,
            orientation=cfg.display.orientation,
            brightness=cfg.display.brightness,
            auto_port=True,
            force_rotate=0,
        )
        if sl.ser:
            displays.append(sl)
            log.info("serial display on %s", sl.port)
    except Exception as e:
        log.error("display init failed: %s", e)
    if not displays:
        log.error("no display")
        stop["flag"] = True
        return

    u = cfg.user
    hw = HardwareMonitor(
        extra_disks=u.disks,
        opencode_url=u.opencode_url,
        opencode_cookie=u.opencode_cookie,
        opencode_token=u.opencode_token,
        network_type=u.network_type,
        ethernet_iface=u.ethernet_iface,
    )
    theme = NeonGengarTheme(
        w, h, cfg.display.orientation,
        gif_dir=str(_HERE / "gif"),
        disks=u.disks,
        network_type=u.network_type,
    )

    period = 1.0 / max(1, cfg.ui.framerate)
    frames = 0
    partial_displays = [d for d in displays if hasattr(d, "send_region")]
    full_displays = [d for d in displays if not hasattr(d, "send_region")]
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
                if frames % 15 == 0:
                    for d in partial_displays:
                        d.send_frame(img)
                else:
                    regions = theme.dirty_regions(snap)
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
        for d in displays:
            try:
                d.close()
            except Exception:
                pass


class PersonalApp:
    def __init__(self):
        self.cfg = Config()
        self.cfg.user.opencode_url = PERSONAL_URL
        self.cfg.user.opencode_cookie = PERSONAL_COOKIE
        self.cfg.user.opencode_token = PERSONAL_TOKEN
        self.cfg.user.disks = ["C:\\", "Z:\\"]
        self.cfg.user.network_type = "wifi"
        self.cfg.user.ethernet_iface = "Беспроводная сеть"
        self.cfg.user.autostart = autostart.is_enabled()
        self.cfg.user.minimize_to_tray = True
        self.cfg.ui.theme = "neon_gengar"
        self.cfg.display.baudrate = 230400

        self.log = get_logger("personal", "INFO")
        self.stop = {"flag": False}
        self.display_thread: threading.Thread | None = None

        self.tray = Tray(
            on_show=self._on_tray_show,
            on_quit=self._on_quit,
        )
        self._build_gui()

    def _build_gui(self):
        self.root = tk.Tk()
        self.root.title("UsbDisplay")
        self.root.geometry("340x180")
        self.root.resizable(False, False)
        try:
            self.root.iconbitmap(default=str(_ICON_DIR / "icon.ico"))
        except Exception:
            pass
        self.root.protocol("WM_DELETE_WINDOW", self._on_close)

        frame = tk.Frame(self.root, padx=16, pady=12)
        frame.pack(fill="both", expand=True)

        self.var_autostart = tk.BooleanVar(value=self.cfg.user.autostart)
        self.var_min_tray = tk.BooleanVar(value=self.cfg.user.minimize_to_tray)

        self.btn = tk.Button(frame, text="Запустить экран", font=("Segoe UI", 13, "bold"),
                             bg="#7c3aed", fg="white", activebackground="#6d28d9",
                             relief="flat", cursor="hand2", command=self._on_start)
        self.btn.pack(fill="x", pady=(0, 10))

        tk.Checkbutton(frame, text="Автозагрузка Windows",
                       variable=self.var_autostart,
                       command=self._on_autostart).pack(anchor="w")
        tk.Checkbutton(frame, text="Сворачивать в трей",
                       variable=self.var_min_tray).pack(anchor="w")

        self.lbl = tk.Label(frame, text="", fg="#888", font=("Segoe UI", 8))
        self.lbl.pack(side="bottom")

    def _on_start(self):
        self.cfg.user.autostart = self.var_autostart.get()
        self.cfg.user.minimize_to_tray = self.var_min_tray.get()
        self.stop["flag"] = True
        if self.display_thread and self.display_thread.is_alive():
            self.display_thread.join(timeout=3)
        self.stop = {"flag": False}
        self.display_thread = threading.Thread(
            target=_display_loop, args=(self.cfg, self.stop, self.log), daemon=True)
        self.display_thread.start()
        self.btn.config(text="Запущено", state="disabled")
        self.lbl.config(text="Дисплей работает")

    def _on_autostart(self):
        if self.var_autostart.get():
            autostart.enable()
        else:
            autostart.disable()

    def _on_tray_show(self):
        self.root.after(0, self._show_window)

    def _show_window(self):
        self.root.deiconify()
        self.root.lift()
        self.root.focus_force()

    def _on_close(self):
        if self.var_min_tray.get():
            self.root.withdraw()
            self.tray.start()
        else:
            self._on_quit()

    def _on_quit(self):
        self.stop["flag"] = True
        try:
            self.tray.stop()
        except Exception:
            pass
        try:
            self.root.quit()
        except Exception:
            pass
        os._exit(0)

    def run(self):
        def _sig(*_):
            self._on_quit()
        try:
            signal.signal(signal.SIGINT, _sig)
            signal.signal(signal.SIGTERM, _sig)
        except Exception:
            pass
        self._on_start()
        self.root.withdraw()
        self.tray.start()
        self.root.mainloop()


if __name__ == "__main__":
    PersonalApp().run()
