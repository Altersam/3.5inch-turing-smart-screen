"""Минималистичная тёмная тема — для теста и как fallback."""
from __future__ import annotations
from datetime import datetime
from PIL import Image, ImageDraw

from .base import BaseTheme
from sensors.hardware import Snapshot
from ui.widgets import text, bar
from ui.fonts import get as font

BG = (12, 12, 18)
FG = (220, 230, 255)
ACC = (120, 180, 255)
PANEL = (22, 24, 34)


class MinimalTheme(BaseTheme):
    def __init__(self, width=480, height=320, orientation="landscape"):
        super().__init__(width, height)
        self.orientation = orientation

    def render(self, snap: Snapshot) -> Image.Image:
        w, h = self.width, self.height
        im = Image.new("RGB", (w, h), BG)
        d = ImageDraw.Draw(im)
        d.rectangle([0, 0, w - 1, h - 1], outline=ACC, width=2)
        text(d, (10, 10), f"{snap.hostname}", 12, ACC, bold=True)
        text(d, (w - 10, 10), datetime.now().strftime("%H:%M:%S"),
             12, ACC, anchor="rt")
        items = [
            ("CPU", f"{snap.cpu.usage:.0f}%  {snap.cpu.freq_ghz:.1f}GHz"),
            ("GPU", f"{snap.gpu.usage:.0f}%"
                    + (f"  {snap.gpu.temp_c:.0f}°C" if snap.gpu.temp_c else "")),
            ("RAM", f"{snap.mem.used_gb:.1f}/{snap.mem.total_gb:.0f}G  "
                    f"{snap.mem.percent:.0f}%"),
            ("DISK", f"{snap.disk.used_gb:.0f}/{snap.disk.total_gb:.0f}G  "
                     f"{snap.disk.percent:.0f}%"),
            ("NET", f"↓{snap.net.rx_bps/1024/1024:.1f}MB/s"
                    f"  ↑{snap.net.tx_bps/1024/1024:.1f}MB/s"),
        ]
        y = 30
        for label, val in items:
            text(d, (10, y), label, 10, ACC, bold=True)
            text(d, (60, y), val, 11, FG)
            y += 18
        # CPU bar
        bar(d, 10, h - 24, w - 20, 8, snap.cpu.usage, fg=ACC, bg=PANEL)
        return im
