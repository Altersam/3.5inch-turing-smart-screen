"""Cyberpunk-тема — жёлто-розовый неон на тёмно-синем фоне."""
from __future__ import annotations
from datetime import datetime
from PIL import Image, ImageDraw

from .base import BaseTheme
from sensors.hardware import Snapshot
from ui.widgets import text, bar, ring


BG_TOP = (8, 6, 28)
BG_BOT = (28, 10, 50)
CYAN = (90, 230, 255)
YEL = (255, 220, 80)
PINK = (255, 90, 200)
PANEL = (18, 10, 36)


class CyberpunkTheme(BaseTheme):
    def __init__(self, width=480, height=320, orientation="landscape"):
        super().__init__(width, height, orientation)

    def _bg(self, w, h):
        im = Image.new("RGB", (w, h), BG_TOP)
        d = ImageDraw.Draw(im)
        for y in range(h):
            t = y / (h - 1)
            c = tuple(int(BG_TOP[i] + (BG_BOT[i] - BG_TOP[i]) * t) for i in range(3))
            d.line([(0, y), (w, y)], fill=c)
        return im

    def _panel(self, d, x, y, w, h):
        d.rectangle([x, y, x + w - 1, y + h - 1], outline=CYAN, width=1)
        d.line([(x, y), (x + 10, y), (x, y), (x, y + 10)], fill=YEL, width=2)
        d.line([(x + w - 1, y), (x + w - 11, y),
                (x + w - 1, y), (x + w - 1, y + 10)], fill=YEL, width=2)
        d.line([(x, y + h - 1), (x + 10, y + h - 1),
                (x, y + h - 1), (x, y + h - 11)], fill=YEL, width=2)
        d.line([(x + w - 1, y + h - 1), (x + w - 11, y + h - 1),
                (x + w - 1, y + h - 1),
                (x + w - 1, y + h - 11)], fill=YEL, width=2)

    def render(self, snap: Snapshot) -> Image.Image:
        w, h = self.width, self.height
        im = self._bg(w, h)
        d = ImageDraw.Draw(im)
        col_w = (w - 30) // 3
        col_h = (h - 30) // 2
        for r in range(2):
            for c in range(3):
                x = 10 + c * (col_w + 5)
                y = 10 + r * (col_h + 5)
                self._panel(d, x, y, col_w, col_h)
        # CPU
        cx, cy = 10 + col_w // 2, 10 + col_h // 2
        text(d, (cx, cy - 4), f"CPU {int(snap.cpu.usage)}%",
             18, YEL, bold=True, anchor="mm")
        ring(d, cx, cy, col_h // 3, snap.cpu.usage, fg=YEL, bg=(40, 30, 10))
        if snap.cpu.temp_c is not None:
            text(d, (cx, cy + col_h // 3 + 10),
                 f"{snap.cpu.temp_c:.0f}°C", 10, PINK, anchor="mm")
        # GPU
        cx2 = 10 + col_w + 5 + col_w // 2
        text(d, (cx2, cy - 4), f"GPU {int(snap.gpu.usage)}%",
             18, CYAN, bold=True, anchor="mm")
        ring(d, cx2, cy, col_h // 3, snap.gpu.usage, fg=CYAN, bg=(20, 30, 40))
        if snap.gpu.temp_c is not None:
            text(d, (cx2, cy + col_h // 3 + 10),
                 f"{snap.gpu.temp_c:.0f}°C", 10, PINK, anchor="mm")
        # RIGHT
        cx3 = 10 + 2 * (col_w + 5) + col_w // 2
        text(d, (cx3, cy - 4), "VRAM", 14, YEL, bold=True, anchor="mm")
        pct = (snap.gpu.vram_used_mb / snap.gpu.vram_total_mb * 100
               if snap.gpu.vram_total_mb else 0)
        text(d, (cx3, cy + 10), f"{snap.gpu.vram_used_mb:.0f}M", 18, CYAN,
             bold=True, anchor="mm")
        text(d, (cx3, cy + 30), f"/{snap.gpu.vram_total_mb:.0f}M",
             10, PINK, anchor="mm")
        bar(d, cx3 - col_w // 2 + 8, cy + col_h // 2 - 14,
            col_w - 16, 8, pct, fg=YEL, bg=PANEL)
        # BOTTOM
        bx = 10
        by = 10 + col_h + 5
        text(d, (bx + 5, by + 6), "RAM", 11, YEL, bold=True)
        text(d, (bx + 50, by + 6),
             f"{snap.mem.used_gb:.1f}/{snap.mem.total_gb:.0f}G  "
             f"{snap.mem.percent:.0f}%", 12, CYAN)
        bar(d, bx + 5, by + 24, col_w - 10, 8,
            snap.mem.percent, fg=CYAN, bg=PANEL)
        text(d, (bx + 5, by + 42), "NET", 11, YEL, bold=True)
        text(d, (bx + 50, by + 42),
             f"↓{snap.net.rx_bps/1024/1024:.1f}  "
             f"↑{snap.net.tx_bps/1024/1024:.1f} MB/s", 11, PINK)
        # DISK
        dx = 10 + col_w + 5
        text(d, (dx + 5, by + 6), "DISK", 11, YEL, bold=True)
        text(d, (dx + 50, by + 6),
             f"{snap.disk.used_gb:.0f}/{snap.disk.total_gb:.0f}G  "
             f"{snap.disk.percent:.0f}%", 12, CYAN)
        bar(d, dx + 5, by + 24, col_w - 10, 8,
            snap.disk.percent, fg=YEL, bg=PANEL)
        if snap.disk.temp_c is not None:
            text(d, (dx + 5, by + 42),
                 f"HDD {snap.disk.temp_c:.0f}°C", 11, PINK)
        else:
            text(d, (dx + 5, by + 42),
                 datetime.now().strftime("%Y/%m/%d %H:%M"), 11, PINK)
        # TIME
        tx = 10 + 2 * (col_w + 5)
        text(d, (tx + col_w // 2, by + col_h // 2 - 6),
             datetime.now().strftime("%H:%M"), 26, YEL, bold=True, anchor="mm")
        text(d, (tx + col_w // 2, by + col_h // 2 + 18),
             datetime.now().strftime("%a %d %b"), 10, CYAN, anchor="mm")
        return im
