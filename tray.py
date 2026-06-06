"""Системный трей (иконка + контекстное меню)."""
import threading
import sys
from pathlib import Path
from typing import Callable, Optional

try:
    import pystray
    from pystray import MenuItem as Item
    from PIL import Image, ImageDraw
    _HAS_PYSTRAY = True
except Exception:
    _HAS_PYSTRAY = False


_HERE = Path(__file__).parent


def _make_icon(size: int = 64) -> "Image.Image":
    """Простая иконка: тёмный фон с фиолетовым кругом."""
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    # фон
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=size // 6,
                        fill=(25, 20, 50, 255))
    # неоновый круг
    d.ellipse([size // 6, size // 6, size * 5 // 6, size * 5 // 6],
              outline=(180, 100, 255, 255), width=max(2, size // 16))
    # точка-глаз
    cx, cy = size // 2, size // 2
    r = size // 10
    d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(255, 60, 60, 255))
    return img


class Tray:
    def __init__(self, on_show: Callable, on_quit: Callable,
                 tooltip: str = "UsbDisplay"):
        self.on_show = on_show
        self.on_quit = on_quit
        self.tooltip = tooltip
        self.icon: Optional["pystray.Icon"] = None
        self._thread: Optional[threading.Thread] = None

    def start(self) -> bool:
        if not _HAS_PYSTRAY:
            return False
        if self.icon is not None:
            return True
        menu = pystray.Menu(
            Item("Показать окно", lambda: self.on_show(), default=True),
            Item("Выход", lambda: self._quit()),
        )
        self.icon = pystray.Icon(
            "UsbDisplay", _make_icon(), self.tooltip, menu)
        self._thread = threading.Thread(target=self.icon.run, daemon=True)
        self._thread.start()
        return True

    def stop(self):
        if self.icon is not None:
            try:
                self.icon.stop()
            except Exception:
                pass
            self.icon = None

    def _quit(self):
        self.stop()
        self.on_quit()
