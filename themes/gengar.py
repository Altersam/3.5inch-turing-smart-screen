"""Тема «Gengar» — неоновый фиолетовый фон с водяным знаком Генгара.
Использует gengar_ref.png из директории скрипта как референс для
силуэта, либо генерирует его динамически через ui.gengar_art."""
from __future__ import annotations
import math
import random
import time
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter

from .base import BaseTheme
from sensors.hardware import Snapshot
from ui.widgets import text, bar, vbar, ring
from ui.fonts import get as font


_NEON_P   = (225, 90, 255)
_NEON_C   = (110, 230, 255)
_NEON_P2  = (255, 160, 235)
_EYE_RED  = (255, 30, 60)
_BG_TOP   = (8, 4, 22)
_BG_MID   = (30, 12, 58)
_BG_BOT   = (58, 20, 100)
_PANEL    = (14, 4, 26)


def _lerp(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))


def _vgrad(w, h, top, bot):
    im = Image.new("RGB", (w, h), top)
    d = ImageDraw.Draw(im)
    for y in range(h):
        d.line([(0, y), (w, y)], fill=_lerp(top, bot, y / (h - 1)))
    return im


def _radial_glow(w, h, cx, cy, radius, color, alpha=160):
    im = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    for r in range(radius, 0, -2):
        a = int(alpha * (1 - r / radius) ** 2)
        d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(*color, a))
    return im.filter(ImageFilter.GaussianBlur(8))


def _make_gengar(w, h):
    """Неоновый силуэт Генгара. Если в директории есть gengar_ref.png —
    используем его, иначе рисуем программно."""
    ref = Path(__file__).parent.parent.parent / "gengar_ref.png"
    if ref.exists():
        try:
            return _make_gengar_from_ref(str(ref), h)
        except Exception:
            pass
    return _make_gengar_polygon(w, h)


def _make_gengar_from_ref(path, target_h):
    g = Image.open(path).convert("RGBA")
    px = g.load()
    w0, h0 = g.size
    for y in range(h0):
        for x in range(w0):
            r, gc, b, a = px[x, y]
            if r > 200 and gc > 200 and b > 200:
                px[x, y] = (0, 0, 0, 0)
            else:
                px[x, y] = (255, 255, 255, 255)
    scale = target_h / h0
    nw = int(w0 * scale)
    g = g.resize((nw, target_h), Image.LANCZOS)
    alpha = g.split()[0]
    bw, bh = g.size

    fill = Image.new("RGBA", (bw, bh), (0, 0, 0, 0))
    df = ImageDraw.Draw(fill)
    df.bitmap((0, 0), alpha, fill=(*_NEON_P, 90))
    fill = fill.filter(ImageFilter.GaussianBlur(2))

    big = Image.new("RGBA", (bw, bh), (0, 0, 0, 0))
    db = ImageDraw.Draw(big)
    db.bitmap((0, 0), alpha, fill=(*_NEON_P, 230))
    big = big.filter(ImageFilter.GaussianBlur(10))

    med = Image.new("RGBA", (bw, bh), (0, 0, 0, 0))
    dm = ImageDraw.Draw(med)
    dm.bitmap((0, 0), alpha, fill=(*_NEON_P, 255))
    med = med.filter(ImageFilter.GaussianBlur(2))

    mask = alpha.point(lambda p: 255 if p > 80 else 0)
    edge = mask.filter(ImageFilter.FIND_EDGES).point(lambda p: min(255, p * 2))
    outline = Image.new("RGBA", (bw, bh), (0, 0, 0, 0))
    do = ImageDraw.Draw(outline)
    do.bitmap((0, 0), edge, fill=(*_NEON_P2, 255))

    comp = Image.new("RGBA", (bw, bh), (0, 0, 0, 0))
    comp.alpha_composite(big)
    comp.alpha_composite(med)
    comp.alpha_composite(fill)
    comp.alpha_composite(outline)

    # циан-неоновое брюхо
    cx, cy = bw // 2, int(bh * 0.62)
    bw2, bh2 = int(bw * 0.42), int(bh * 0.30)
    belly_glow = Image.new("RGBA", (bw, bh), (0, 0, 0, 0))
    dby = ImageDraw.Draw(belly_glow)
    dby.ellipse([cx - bw2 // 2, cy - bh2 // 2, cx + bw2 // 2, cy + bh2 // 2],
                outline=(*_NEON_C, 220), width=2)
    belly_glow = belly_glow.filter(ImageFilter.GaussianBlur(3))
    comp.alpha_composite(belly_glow)

    # красные глаза
    eye_y = int(bh * 0.40)
    eg = Image.new("RGBA", (bw, bh), (0, 0, 0, 0))
    de = ImageDraw.Draw(eg)
    for sign in (-1, 1):
        ex = bw // 2 + sign * int(bw * 0.13)
        for r in range(20, 0, -2):
            a = int(140 * (1 - r / 20) ** 2)
            de.ellipse([ex - r, eye_y - r // 2, ex + r, eye_y + r // 2],
                       fill=(*_EYE_RED, a))
    eg = eg.filter(ImageFilter.GaussianBlur(3))
    comp.alpha_composite(eg)
    ec = Image.new("RGBA", (bw, bh), (0, 0, 0, 0))
    dec = ImageDraw.Draw(ec)
    for sign in (-1, 1):
        ex = bw // 2 + sign * int(bw * 0.13)
        pts = []
        for t in range(0, 41):
            a = -math.pi + t / 40 * math.pi
            x = ex + math.cos(a) * 12
            y = eye_y + math.sin(a) * 9
            pts.append((x, y))
        dec.polygon(pts, fill=(*_EYE_RED, 255))
        dec.ellipse([ex - 3, eye_y - 2, ex + 1, eye_y + 2],
                    fill=(255, 255, 255, 240))
    comp.alpha_composite(ec)
    return comp


def _make_gengar_polygon(w, h):
    """Запасной вариант: рисуем Генгара полигонами."""
    cx, cy = w // 2, int(h * 0.40)
    body_r = int(min(w, h) * 0.28)
    pts = []
    # нижняя дуга
    for i in range(41):
        a = math.radians(180 + i * 180 / 40)
        pts.append((cx + math.cos(a) * body_r,
                    cy - math.sin(a) * body_r))
    # шипы наверх
    spike_xs = [body_r - 10, body_r - 30, body_r - 5, body_r - 30, body_r - 10]
    spike_tops = [cy - body_r - 30, cy - body_r - 65, cy - body_r - 80,
                  cy - body_r - 65, cy - body_r - 30]
    for sx, sy in zip(spike_xs, spike_tops):
        pts.append((cx - sx, sy))
    for sx, sy in zip(reversed(spike_xs), reversed(spike_tops)):
        pts.append((cx + sx, sy))
    pts.append((cx + body_r, cy))

    comp = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    glow = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    dg = ImageDraw.Draw(glow)
    dg.polygon(pts, outline=(*_NEON_P, 220))
    glow = glow.filter(ImageFilter.GaussianBlur(10))
    comp.alpha_composite(glow)
    mid = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    dm = ImageDraw.Draw(mid)
    dm.polygon(pts, outline=(*_NEON_P, 255), width=2)
    comp.alpha_composite(mid)
    return comp


def _panel(d, x, y, w, h, title=None):
    d.rectangle([x + 5, y + 5, x + w - 5, y + h - 5],
                fill=(*_PANEL, 215), outline=(*_NEON_P, 235), width=1)
    # угловые засечки
    L = 8
    d.line([(x + 5, y + 5 + L), (x + 5, y + 5), (x + 5 + L, y + 5)],
           fill=_NEON_P2, width=2)
    d.line([(x + w - 5, y + 5 + L), (x + w - 5, y + 5), (x + w - 5 - L, y + 5)],
           fill=_NEON_P2, width=2)
    d.line([(x + 5, y + h - 5 - L), (x + 5, y + h - 5), (x + 5 + L, y + h - 5)],
           fill=_NEON_P2, width=2)
    d.line([(x + w - 5, y + h - 5 - L), (x + w - 5, y + h - 5),
            (x + w - 5 - L, y + h - 5)], fill=_NEON_P2, width=2)
    if title:
        text(d, (x + 10, y + 8), title, 9, _NEON_P2, bold=True)


class GengarTheme(BaseTheme):
    def __init__(self, width=480, height=320, orientation="landscape"):
        super().__init__(width, height, orientation)
        self._gengar = _make_gengar(width, height)

    def render(self, snap: Snapshot) -> Image.Image:
        w, h = self.width, self.height
        base = _vgrad(w, h, _BG_TOP, _BG_BOT).convert("RGBA")
        base.alpha_composite(_radial_glow(w, h, w // 2, h + 40, 280, _BG_MID, 230))
        base.alpha_composite(_radial_glow(w, h, w // 2, 0, 160, (90, 40, 130), 180))

        # частицы
        rnd = random.Random()
        rnd.seed(int(time.time() * 3) % 9999)
        p = Image.new("RGBA", (w, h), (0, 0, 0, 0))
        dp = ImageDraw.Draw(p)
        for _ in range(70):
            x = rnd.randint(0, w)
            y = rnd.randint(0, h)
            r = rnd.choice([1, 1, 1, 2, 2, 3])
            a = rnd.randint(60, 200)
            col = rnd.choice([(190, 100, 230), _NEON_P2, _NEON_C, (255, 255, 255)])
            dp.ellipse([x - r, y - r, x + r, y + r], fill=(*col, a))
        p = p.filter(ImageFilter.GaussianBlur(0.5))
        base.alpha_composite(p)

        # Генгар по центру
        gx = (w - self._gengar.size[0]) // 2
        gy = (h - self._gengar.size[1]) // 2
        base.alpha_composite(self._gengar, (gx, gy))

        d = ImageDraw.Draw(base, "RGBA")

        if self.orientation == "landscape":
            self._render_landscape(d, base, snap)
        else:
            self._render_portrait(d, base, snap)
        return base.convert("RGB")

    def _render_landscape(self, d, im, snap: Snapshot):
        w, h = im.size
        # 3 колонки по 2 строки
        col_w = (w - 40) // 3
        col_h = (h - 30) // 2
        for r in range(2):
            for c in range(3):
                x = 10 + c * (col_w + 5)
                y = 12 + r * (col_h + 5)
                _panel(d, x, y, col_w, col_h)
        # Заголовки
        _panel_titles = ["NET / TIME", "CPU", "GPU",
                         "RAM", "DISK / TEMP", "GPU RAM"]
        idx = 0
        for r in range(2):
            for c in range(3):
                x = 10 + c * ((w - 40) // 3 + 5)
                y = 12 + r * (col_h + 5)
                text(d, (x + 10, y + 8), _panel_titles[idx], 8, _NEON_P2, bold=True)
                idx += 1
        # Заполняем
        col_w = (w - 40) // 3
        col_h = (h - 30) // 2
        p0 = (10, 12)
        p1 = (10 + col_w + 5, 12)
        p2 = (10 + 2 * (col_w + 5), 12)
        p3 = (10, 12 + col_h + 5)
        p4 = (10 + col_w + 5, 12 + col_h + 5)
        p5 = (10 + 2 * (col_w + 5), 12 + col_h + 5)

        self._draw_net(d, *p0, col_w, col_h, snap)
        self._draw_cpu(d, *p1, col_w, col_h, snap)
        self._draw_gpu(d, *p2, col_w, col_h, snap)
        self._draw_ram(d, *p3, col_w, col_h, snap)
        self._draw_disk(d, *p4, col_w, col_h, snap)
        self._draw_gpuram(d, *p5, col_w, col_h, snap)

    def _render_portrait(self, d, im, snap: Snapshot):
        w, h = im.size
        col_w = (w - 30) // 2
        row_h = (h - 40) // 3
        for r in range(3):
            for c in range(2):
                x = 10 + c * (col_w + 5)
                y = 12 + r * (row_h + 5)
                _panel(d, x, y, col_w, row_h)
        titles = ["CPU", "GPU", "RAM", "DISK", "NET", "TIME"]
        idx = 0
        for r in range(3):
            for c in range(2):
                x = 10 + c * (col_w + 5)
                y = 12 + r * (row_h + 5)
                text(d, (x + 10, y + 8), titles[idx], 8, _NEON_P2, bold=True)
                idx += 1
        col_w = (w - 30) // 2
        row_h = (h - 40) // 3
        panels = [(10, 12), (10 + col_w + 5, 12),
                  (10, 12 + row_h + 5), (10 + col_w + 5, 12 + row_h + 5),
                  (10, 12 + 2 * (row_h + 5)), (10 + col_w + 5, 12 + 2 * (row_h + 5))]
        self._draw_cpu(d, *panels[0], col_w, row_h, snap)
        self._draw_gpu(d, *panels[1], col_w, row_h, snap)
        self._draw_ram(d, *panels[2], col_w, row_h, snap)
        self._draw_disk(d, *panels[3], col_w, row_h, snap)
        self._draw_net(d, *panels[4], col_w, row_h, snap)
        self._draw_time(d, *panels[5], col_w, row_h, snap)

    # ----- контент панелей -----
    def _draw_cpu(self, d, x, y, w, h, snap):
        cx = x + w // 2
        text(d, (x + w // 2, y + 22), f"{int(snap.cpu.usage)}%",
             28, _NEON_P, bold=True, anchor="mm")
        ring(d, cx, y + h // 2 + 6, min(w, h) // 3 - 6, snap.cpu.usage)
        text(d, (x + w // 2, y + h - 22),
             f"{snap.cpu.freq_ghz:.1f}GHz" if snap.cpu.freq_ghz else "—",
             9, _NEON_C, anchor="mm")
        if snap.cpu.temp_c is not None:
            text(d, (x + w - 12, y + h - 14),
                 f"{snap.cpu.temp_c:.0f}°", 9, _EYE_RED, anchor="rm")

    def _draw_gpu(self, d, x, y, w, h, snap):
        cx = x + w // 2
        text(d, (x + w // 2, y + 22), f"{int(snap.gpu.usage)}%",
             28, _NEON_P, bold=True, anchor="mm")
        ring(d, cx, y + h // 2 + 6, min(w, h) // 3 - 6, snap.gpu.usage)
        if snap.gpu.temp_c is not None:
            text(d, (x + w // 2, y + h - 22),
                 f"{snap.gpu.temp_c:.0f}°C", 9, _EYE_RED, anchor="mm")
        elif snap.gpu.power_w is not None:
            text(d, (x + w // 2, y + h - 22),
                 f"{snap.gpu.power_w:.0f}W", 9, _NEON_C, anchor="mm")

    def _draw_ram(self, d, x, y, w, h, snap):
        text(d, (x + w // 2, y + 22),
             f"{snap.mem.used_gb:.1f}/{snap.mem.total_gb:.0f}G",
             16, _NEON_P, bold=True, anchor="mm")
        text(d, (x + w // 2, y + 46), f"{snap.mem.percent:.0f}%",
             14, _NEON_C, anchor="mm")
        bar(d, x + 14, y + h - 24, w - 28, 10, snap.mem.percent,
            fg=(140, 90, 255), bg=(40, 30, 70))

    def _draw_disk(self, d, x, y, w, h, snap):
        text(d, (x + w // 2, y + 22),
             f"{snap.disk.used_gb:.0f}/{snap.disk.total_gb:.0f}G",
             16, _NEON_P, bold=True, anchor="mm")
        text(d, (x + w // 2, y + 46), f"{snap.disk.percent:.0f}%",
             14, _NEON_C, anchor="mm")
        bar(d, x + 14, y + h - 24, w - 28, 10, snap.disk.percent,
            fg=(90, 230, 220), bg=(30, 50, 70))
        if snap.disk.temp_c is not None:
            text(d, (x + w - 12, y + h - 40),
                 f"{snap.disk.temp_c:.0f}°", 9, _EYE_RED, anchor="rm")

    def _draw_net(self, d, x, y, w, h, snap):
        rx_mb = snap.net.rx_bps / (1024 * 1024)
        tx_mb = snap.net.tx_bps / (1024 * 1024)
        text(d, (x + 10, y + 22), "↓", 12, _NEON_C)
        text(d, (x + 24, y + 22), f"{rx_mb:.1f} M/s", 14, _NEON_P, bold=True)
        text(d, (x + 10, y + 44), "↑", 12, _NEON_C)
        text(d, (x + 24, y + 44), f"{tx_mb:.1f} M/s", 14, _NEON_P, bold=True)
        bar(d, x + 10, y + 64, w - 20, 8, min(100, rx_mb * 2),
            fg=(110, 230, 255), bg=(30, 50, 80))
        bar(d, x + 10, y + 80, w - 20, 8, min(100, tx_mb * 2),
            fg=(255, 160, 230), bg=(50, 30, 70))
        from datetime import datetime
        now = datetime.now()
        text(d, (x + w // 2, y + h - 14),
             now.strftime("%Y/%m/%d %H:%M"), 9, _NEON_C, anchor="mm")

    def _draw_gpuram(self, d, x, y, w, h, snap):
        text(d, (x + w // 2, y + 22),
             f"{snap.gpu.vram_used_mb:.0f}M",
             18, _NEON_P, bold=True, anchor="mm")
        text(d, (x + w // 2, y + 44),
             f"/{snap.gpu.vram_total_mb:.0f}M", 12, _NEON_C, anchor="mm")
        if snap.gpu.vram_total_mb > 0:
            pct = snap.gpu.vram_used_mb / snap.gpu.vram_total_mb * 100
        else:
            pct = 0
        bar(d, x + 14, y + h - 24, w - 28, 10, pct,
            fg=(255, 200, 100), bg=(60, 50, 30))

    def _draw_time(self, d, x, y, w, h, snap):
        from datetime import datetime
        now = datetime.now()
        text(d, (x + w // 2, y + h // 2 - 10),
             now.strftime("%H:%M:%S"), 22, _NEON_P, bold=True, anchor="mm")
        text(d, (x + w // 2, y + h // 2 + 18),
             now.strftime("%a %d %b"), 11, _NEON_C, anchor="mm")
