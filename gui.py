"""Tkinter-форма конфигурации. Вызывается из app.py."""
import os
import sys
import threading
import tkinter as tk
from tkinter import ttk, messagebox
from pathlib import Path

# чтобы импорт из корня работал и из app.py, и при прямом запуске
_HERE = Path(__file__).parent
if str(_HERE) not in sys.path:
    sys.path.insert(0, str(_HERE))

from sensors.hardware import list_disk_mounts
from core.config import Config, UserConfig, default_config_path
import autostart


class ConfigGUI:
    def __init__(self, cfg: Config, on_start=None, on_tray=None):
        """cfg: Config, on_start: callback(cfg) при клике "Запустить экран".
        on_tray: callback() при сворачивании в трей."""
        self.cfg = cfg
        self.on_start = on_start
        self.on_tray = on_tray
        self.root = tk.Tk()
        self.root.title("UsbDisplay — конфигурация")
        self.root.geometry("520x640")
        self.root.minsize(480, 580)
        # иконка (если есть)
        try:
            self.root.iconbitmap(default=str(_HERE / "icon.ico"))
        except Exception:
            pass
        # перехват закрытия — сворачиваем в трей
        self.root.protocol("WM_DELETE_WINDOW", self._on_close)
        self._build_ui()
        self._populate()

    # ---------- UI ----------
    def _build_ui(self):
        pad = {"padx": 8, "pady": 4}

        # === OpenCode ===
        f_oc = ttk.LabelFrame(self.root, text="OpenCode")
        f_oc.pack(fill="x", padx=10, pady=6)
        ttk.Label(f_oc, text="Workspace URL:").grid(row=0, column=0, sticky="w", **pad)
        self.var_oc_url = tk.StringVar()
        ttk.Entry(f_oc, textvariable=self.var_oc_url, width=60).grid(
            row=0, column=1, columnspan=2, sticky="we", **pad)
        ttk.Label(f_oc, text="Cookie (auth):").grid(row=1, column=0, sticky="w", **pad)
        self.var_oc_cookie = tk.StringVar()
        e_cookie = ttk.Entry(f_oc, textvariable=self.var_oc_cookie, width=60, show="•")
        e_cookie.grid(row=1, column=1, columnspan=2, sticky="we", **pad)
        ttk.Label(f_oc, text="Bearer token:").grid(row=2, column=0, sticky="w", **pad)
        self.var_oc_token = tk.StringVar()
        e_token = ttk.Entry(f_oc, textvariable=self.var_oc_token, width=60, show="•")
        e_token.grid(row=2, column=1, columnspan=2, sticky="we", **pad)
        f_oc.columnconfigure(1, weight=1)

        # === Диски ===
        f_dk = ttk.LabelFrame(self.root, text="Диски (1 или 2)")
        f_dk.pack(fill="x", padx=10, pady=6)
        self.var_dk_count = tk.IntVar(value=2)
        rb1 = ttk.Radiobutton(f_dk, text="1 диск", variable=self.var_dk_count,
                              value=1, command=self._on_dk_count)
        rb1.grid(row=0, column=0, sticky="w", **pad)
        rb2 = ttk.Radiobutton(f_dk, text="2 диска", variable=self.var_dk_count,
                              value=2, command=self._on_dk_count)
        rb2.grid(row=0, column=1, sticky="w", **pad)
        ttk.Label(f_dk, text="Диск 1:").grid(row=1, column=0, sticky="w", **pad)
        self.cb_dk1 = ttk.Combobox(f_dk, width=20, state="readonly")
        self.cb_dk1.grid(row=1, column=1, sticky="w", **pad)
        ttk.Label(f_dk, text="Диск 2:").grid(row=2, column=0, sticky="w", **pad)
        self.cb_dk2 = ttk.Combobox(f_dk, width=20, state="readonly")
        self.cb_dk2.grid(row=2, column=1, sticky="w", **pad)

        # === Сеть ===
        f_net = ttk.LabelFrame(self.root, text="Сеть")
        f_net.pack(fill="x", padx=10, pady=6)
        self.var_net = tk.StringVar(value="wifi")
        ttk.Radiobutton(f_net, text="Wi-Fi (SSID, сигнал)",
                        variable=self.var_net, value="wifi",
                        command=self._on_net).grid(row=0, column=0, sticky="w", **pad)
        ttk.Radiobutton(f_net, text="Ethernet (имя адаптера)",
                        variable=self.var_net, value="ethernet",
                        command=self._on_net).grid(row=1, column=0, sticky="w", **pad)
        ttk.Label(f_net, text="Интерфейс:").grid(row=2, column=0, sticky="w", **pad)
        self.cb_iface = ttk.Combobox(f_net, width=30, state="readonly")
        self.cb_iface.grid(row=2, column=1, sticky="we", **pad)
        f_net.columnconfigure(1, weight=1)

        # === Дисплей ===
        f_disp = ttk.LabelFrame(self.root, text="Дисплей")
        f_disp.pack(fill="x", padx=10, pady=6)
        ttk.Label(f_disp, text="Порт:").grid(row=0, column=0, sticky="w", **pad)
        self.var_port = tk.StringVar(value="AUTO")
        self.cb_port = ttk.Combobox(f_disp, width=15, state="normal",
                                    values=["AUTO", "COM3", "COM4", "COM5", "COM6", "COM7"])
        self.cb_port.grid(row=0, column=1, sticky="w", **pad)
        ttk.Label(f_disp, text="Baud:").grid(row=0, column=2, sticky="e", **pad)
        self.var_baud = tk.StringVar(value="230400")
        ttk.Combobox(f_disp, textvariable=self.var_baud, width=10, state="readonly",
                     values=["115200", "230400"]).grid(row=0, column=3, sticky="w", **pad)
        ttk.Label(f_disp, text="Тема:").grid(row=1, column=0, sticky="w", **pad)
        self.var_theme = tk.StringVar(value="neon_gengar")
        ttk.Combobox(f_disp, textvariable=self.var_theme, width=15, state="readonly",
                     values=["neon_gengar", "gengar", "minimal", "cyberpunk"]).grid(
            row=1, column=1, sticky="w", **pad)
        ttk.Label(f_disp, text="Поворот:").grid(row=1, column=2, sticky="e", **pad)
        self.var_rotate = tk.StringVar(value="0")
        ttk.Combobox(f_disp, textvariable=self.var_rotate, width=5, state="readonly",
                     values=["0", "90", "180", "270"]).grid(row=1, column=3, sticky="w", **pad)

        # === Автозапуск и трей ===
        f_sys = ttk.LabelFrame(self.root, text="Система")
        f_sys.pack(fill="x", padx=10, pady=6)
        self.var_autostart = tk.BooleanVar(value=False)
        ttk.Checkbutton(f_sys, text="Запускать при загрузке Windows",
                        variable=self.var_autostart,
                        command=self._on_autostart_toggle).grid(
            row=0, column=0, sticky="w", **pad)
        self.var_min_tray = tk.BooleanVar(value=True)
        ttk.Checkbutton(f_sys, text="Сворачивать в трей при закрытии",
                        variable=self.var_min_tray).grid(
            row=1, column=0, sticky="w", **pad)

        # === Кнопки ===
        f_btn = ttk.Frame(self.root)
        f_btn.pack(fill="x", padx=10, pady=10)
        self.btn_start = ttk.Button(f_btn, text="▶ Запустить экран",
                                    command=self._on_start_click)
        self.btn_start.pack(side="left", padx=4)
        ttk.Button(f_btn, text="В трей", command=self._on_tray).pack(side="left", padx=4)
        ttk.Button(f_btn, text="Сохранить", command=self._on_save).pack(side="left", padx=4)
        ttk.Button(f_btn, text="Открыть папку",
                   command=lambda: os.startfile(str(_HERE))).pack(side="right", padx=4)

        # === Статус ===
        self.lbl_status = ttk.Label(self.root, text="", foreground="#888")
        self.lbl_status.pack(fill="x", padx=10, pady=4)

    def _populate(self):
        u = self.cfg.user
        # диски
        disks = list_disk_mounts()
        self.cb_dk1["values"] = disks
        self.cb_dk2["values"] = disks
        cur = u.disks or ["C:\\", "Z:\\"]
        if len(cur) >= 1 and cur[0] in disks:
            self.cb_dk1.set(cur[0])
        else:
            self.cb_dk1.set(disks[0] if disks else "C:\\")
        if len(cur) >= 2 and cur[1] in disks:
            self.cb_dk2.set(cur[1])
        else:
            self.cb_dk2.set(disks[1] if len(disks) > 1 else "C:\\")
        self.var_dk_count.set(min(2, len(cur)) if cur else 2)
        self._on_dk_count()
        # opencode
        self.var_oc_url.set(u.opencode_url or "")
        self.var_oc_cookie.set(u.opencode_cookie or "")
        self.var_oc_token.set(u.opencode_token or "")
        # сеть
        self.var_net.set(u.network_type or "wifi")
        self._refresh_ifaces()
        if u.ethernet_iface:
            self.cb_iface.set(u.ethernet_iface)
        # дисплей
        self.var_port.set(self.cfg.display.port)
        self.var_baud.set(str(self.cfg.display.baudrate))
        self.var_theme.set(self.cfg.ui.theme)
        self.var_rotate.set("0")
        # система
        self.var_autostart.set(autostart.is_enabled())
        self.var_min_tray.set(bool(u.minimize_to_tray))

    def _refresh_ifaces(self):
        ifaces = []
        try:
            import psutil
            ifaces = [n for n in psutil.net_if_addrs().keys()
                      if not n.lower().startswith("loopback")]
        except Exception:
            pass
        if not ifaces:
            ifaces = ["Ethernet"]
        self.cb_iface["values"] = ifaces
        if self.cb_iface.get() not in ifaces:
            self.cb_iface.set(ifaces[0])

    def _on_dk_count(self):
        n = self.var_dk_count.get()
        state2 = "readonly" if n >= 2 else "disabled"
        self.cb_dk2.configure(state=state2)
        if n == 1:
            # диск 2 = тот же что 1, чтоб неактивный не смущал
            pass

    def _on_net(self):
        # ничего — значения подставляются при сохранении
        pass

    def _on_autostart_toggle(self):
        want = self.var_autostart.get()
        if want:
            ok = autostart.enable()
            if not ok:
                messagebox.showerror(
                    "Автозапуск",
                    "Не удалось добавить запись в реестр. Проверьте права.")
                self.var_autostart.set(False)
        else:
            autostart.disable()

    def _collect_user(self) -> UserConfig:
        n = self.var_dk_count.get()
        d1 = self.cb_dk1.get().strip()
        d2 = self.cb_dk2.get().strip()
        disks = [d1] if n == 1 else [d1, d2]
        if d2 and d2 == d1:
            disks = [d1]
        return UserConfig(
            opencode_url=self.var_oc_url.get().strip(),
            opencode_cookie=self.var_oc_cookie.get().strip(),
            opencode_token=self.var_oc_token.get().strip(),
            disks=disks,
            network_type=self.var_net.get(),
            ethernet_iface=self.cb_iface.get().strip(),
            autostart=self.var_autostart.get(),
            minimize_to_tray=self.var_min_tray.get(),
        )

    def _on_save(self):
        self.cfg.user = self._collect_user()
        self.cfg.display.port = self.var_port.get().strip() or "AUTO"
        self.cfg.display.baudrate = int(self.var_baud.get())
        self.cfg.ui.theme = self.var_theme.get()
        try:
            self.cfg.save(default_config_path())
            # опционально — в файлы opencode_*.txt (для совместимости)
            if self.cfg.user.opencode_cookie:
                (_HERE / "opencode_cookie.txt").write_text(
                    self.cfg.user.opencode_cookie, encoding="utf-8")
            if self.cfg.user.opencode_token:
                (_HERE / "opencode_token.txt").write_text(
                    self.cfg.user.opencode_token, encoding="utf-8")
            self._set_status("✓ Сохранено")
        except Exception as e:
            messagebox.showerror("Сохранение", f"Ошибка: {e}")

    def _on_start_click(self):
        self._on_save()
        if self.on_start:
            self.on_start(self.cfg)
        self._set_status("▶ Дисплей запущен")

    def _on_tray(self):
        if self.on_tray:
            self.on_tray()
        self.root.withdraw()

    def _on_close(self):
        if self.var_min_tray.get():
            self._on_tray()
        else:
            self.root.quit()

    def _set_status(self, txt: str):
        self.lbl_status.config(text=txt)

    def show(self):
        self.root.deiconify()
        self.root.lift()

    def hide(self):
        self.root.withdraw()

    def mainloop(self):
        self.root.mainloop()
