"""Windows-автозапуск через реестр.

Добавляет/удаляет запись в HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Run
с командой запуска pythonw.exe + app.py (без консольного окна).
"""
import os
import sys
import winreg
from pathlib import Path

REG_PATH = r"Software\Microsoft\Windows\CurrentVersion\Run"
APP_NAME = "UsbDisplay"


def _is_windows() -> bool:
    return sys.platform == "win32"


def _python_exe() -> str:
    """Используем pythonw.exe (без консольного окна)."""
    py = Path(sys.executable)
    if py.name.lower() == "python.exe":
        candidate = py.with_name("pythonw.exe")
        if candidate.exists():
            return str(candidate)
    return str(py)


def _project_path() -> Path:
    """Папка, где лежит app.py / main.py."""
    return Path(__file__).parent.resolve()


def get_command() -> str:
    """Команда, которую пропишем в реестр: 'pythonw.exe "app.py" [--autostart]'."""
    py = _python_exe()
    app_py = str((_project_path() / "app.py").resolve())
    # кавычки вокруг путей на случай пробелов
    return f'"{py}" "{app_py}" --autostart'


def is_enabled() -> bool:
    if not _is_windows():
        return False
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, REG_PATH,
                            0, winreg.KEY_READ) as key:
            value, _ = winreg.QueryValueEx(key, APP_NAME)
            return bool(value)
    except FileNotFoundError:
        return False
    except Exception:
        return False


def enable() -> bool:
    if not _is_windows():
        return False
    try:
        cmd = get_command()
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, REG_PATH,
                            0, winreg.KEY_SET_VALUE) as key:
            winreg.SetValueEx(key, APP_NAME, 0, winreg.REG_SZ, cmd)
        return True
    except Exception:
        return False


def disable() -> bool:
    if not _is_windows():
        return False
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, REG_PATH,
                            0, winreg.KEY_SET_VALUE) as key:
            winreg.DeleteValue(key, APP_NAME)
        return True
    except FileNotFoundError:
        return True   # уже нет — считаем успехом
    except Exception:
        return False
