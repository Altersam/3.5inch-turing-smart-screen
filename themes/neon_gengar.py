"""Неоновая тема «Gen v3» — статус-бары вместо тахометров, крупный текст.

Углы (зарезервировано под GIF):
  - top-left:  Гастли 100×100
  - top-right: Хантер 100×100
  - bottom-right: Генгар 100×100

Параметры:
  - Top middle: OPENCODE balance + время (крупно)
  - Middle row: CPU | GPU | RAM | WiFi (большие числа + бар)
  - Bottom-left: DISK C | DISK Z
"""
from __future__ import annotations
import math
import time
import random
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter, ImageFont

from .base import BaseTheme
from sensors.hardware import Snapshot
from ui.widgets import text as draw_text
from ui.fonts import get as font


# палитра Генгара
NEON_P   = (225, 90, 255)
NEON_P2  = (255, 160, 235)
NEON_C   = (110, 230, 255)
NEON_W   = (240, 240, 255)
EYE_RED  = (255, 40, 80)
BG_TOP   = (10, 4, 24)
BG_MID   = (32, 10, 64)
BG_BOT   = (60, 18, 100)
PANEL    = (16, 6, 30)


GIF_DIR = Path(__file__).resolve().parent.parent / "gif"


def _find_assets():
    g = {}
    for name, fname in [("gastly", "gastly.gif"),
                       ("haunter", "haunter.gif"),
                       ("gengar", "gengar.gif")]:
        p = GIF_DIR / fname
        g[name] = p if p.exists() else None
    return g["gastly"], g["haunter"], g["gengar"]


def _disk_pct(snap, mount: str) -> float:
    m = mount.rstrip("/\\") + "\\"
    for k, v in snap.disks.items():
        if k.rstrip("/\\") + "\\" == m:
            return v.percent
    if snap.disk.mount.rstrip("/\\") + "\\" == m:
        return snap.disk.percent
    return 0.0


def _load_frames(path: Path, size: int):
    if not path:
        return []
    try:
        im = Image.open(path)
    except Exception:
        return []
    out = []
    if getattr(im, "is_animated", False):
        n = getattr(im, "n_frames", 1)
        for i in range(n):
            im.seek(i)
            fr = im.convert("RGBA").copy()
            fr = _to_silhouette(fr)
            fr = fr.resize((size, size), Image.LANCZOS)
            out.append(fr)
    else:
        fr = im.convert("RGBA").copy()
        fr = _to_silhouette(fr)
        fr = fr.resize((size, size), Image.LANCZOS)
        out.append(fr)
    return out


def _to_silhouette(img: Image.Image) -> Image.Image:
    if img.mode != "RGBA":
        img = img.convert("RGBA")
    rgb = img.convert("RGB")
    px = rgb.load()
    a_px = img.split()[3].load()
    w, h = rgb.size
    out = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    op = out.load()
    for y in range(h):
        for x in range(w):
            r, g, b = px[x, y]
            a = a_px[x, y]
            if r > 230 and g > 230 and b > 230:
                op[x, y] = (0, 0, 0, 0)
            elif a < 30:
                op[x, y] = (0, 0, 0, 0)
            else:
                op[x, y] = (r, g, b, a)
    return out


def _neonify(img: Image.Image, color=NEON_P, glow_color=NEON_P2) -> Image.Image:
    w, h = img.size
    alpha = img.split()[3].point(lambda p: 255 if p > 30 else 0)
    big = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    big.paste((*glow_color, 200), (0, 0), alpha)
    big = big.filter(ImageFilter.GaussianBlur(8))
    med = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    med.paste((*color, 255), (0, 0), alpha)
    med = med.filter(ImageFilter.GaussianBlur(1.5))
    edge = alpha.filter(ImageFilter.FIND_EDGES).point(lambda p: min(255, p * 2))
    outline = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    outline.paste((*glow_color, 255), (0, 0), edge)
    comp = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    comp.alpha_composite(big)
    comp.alpha_composite(med)
    comp.alpha_composite(outline)
    comp.alpha_composite(img)
    return comp


def _vgrad(w, h, top, bot):
    im = Image.new("RGB", (w, h), top)
    d = ImageDraw.Draw(im)
    for y in range(h):
        t = y / (h - 1)
        c = tuple(int(top[i] + (bot[i] - top[i]) * t) for i in range(3))
        d.line([(0, y), (w, y)], fill=c)
    return im


def _radial_glow(w, h, cx, cy, radius, color, alpha=160):
    im = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    for r in range(radius, 0, -2):
        a = int(alpha * (1 - r / radius) ** 2)
        d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(*color, a))
    return im.filter(ImageFilter.GaussianBlur(8))


def _panel(d, x, y, w, h, title=None, title_color=None, title_size=12):
    d.rectangle([x + 3, y + 3, x + w - 3, y + h - 3],
                fill=(*PANEL, 220), outline=(*NEON_P, 240), width=1)
    L = 8
    c = title_color or NEON_P2
    d.line([(x + 3, y + 3 + L), (x + 3, y + 3), (x + 3 + L, y + 3)],
           fill=c, width=2)
    d.line([(x + w - 3, y + 3 + L), (x + w - 3, y + 3), (x + w - 3 - L, y + 3)],
           fill=c, width=2)
    d.line([(x + 3, y + h - 3 - L), (x + 3, y + h - 3), (x + 3 + L, y + h - 3)],
           fill=c, width=2)
    d.line([(x + w - 3, y + h - 3 - L), (x + w - 3, y + h - 3),
            (x + w - 3 - L, y + h - 3)], fill=c, width=2)
    if title:
        draw_text(d, (x + 8, y + 6), title, title_size, c, bold=True)


def _bar(d, x, y, w, h, percent, fg, bg=(40, 30, 60)):
    if percent < 0: percent = 0
    if percent > 100: percent = 100
    pad = 1
    d.rounded_rectangle([x, y, x + w, y + h], radius=h // 2,
                        fill=bg, outline=(120, 110, 150), width=1)
    fill_w = max(1, int((w - 2 * pad) * percent / 100))
    if fill_w > 2:
        d.rounded_rectangle(
            [x + pad, y + pad, x + pad + fill_w, y + h - pad],
            radius=(h - 2 * pad) // 2, fill=fg)
    # процент справа внутри бара (мелко)
    pct_text = f"{int(percent)}%"
    fnt = font("C:/Windows/Fonts/seguisb.ttf", max(7, h - 2))
    draw_text(d, (x + w - 6, y + h // 2), pct_text, max(7, h - 2),
              (0, 0, 0, 255), anchor="rm")


def _trunc(draw, text, max_w, fnt):
    if not text:
        return ""
    try:
        if draw.textlength(text, font=fnt) <= max_w:
            return text
    except Exception:
        pass
    while text and text[-1] in " .":
        text = text[:-1]
    while text:
        text = text[:-1]
        cand = text + "…"
        try:
            if draw.textlength(cand, font=fnt) <= max_w:
                return cand
        except Exception:
            return text
    return ""


GIF_SIZE = 100


class NeonGengarTheme(BaseTheme):
    def __init__(self, width=480, height=320, orientation="landscape",
                 gif_dir: str | None = None,
                 disks: list[str] | None = None,
                 network_type: str = "wifi"):
        super().__init__(width, height, orientation)
        global GIF_DIR
        if gif_dir:
            GIF_DIR = Path(gif_dir)
        p_gastly, p_haunter, p_gengar = _find_assets()
        self.frames = {
            "gastly":  _load_frames(p_gastly, GIF_SIZE),
            "haunter": _load_frames(p_haunter, GIF_SIZE),
            "gengar":  _load_frames(p_gengar, GIF_SIZE),
        }
        self.neon = {}
        for k, fl in self.frames.items():
            if fl:
                color = {"gastly": (140, 130, 255),
                         "haunter": (200, 100, 230),
                         "gengar": NEON_P}[k]
                self.neon[k] = [_neonify(f, color=color) for f in fl]
            else:
                self.neon[k] = [self._placeholder(k)]
        self._t0 = time.time()
        # пользовательские настройки
        self.disks = list(disks) if disks else ["C:\\", "Z:\\"]
        self.network_type = network_type
        # tracking предыдущих значений для partial updates
        self._last = {}
        self._gif_idx = {"gastly": -1, "haunter": -1, "gengar": -1}
        # round-robin индекс для GIF-углов
        self._gif_rr = 0
        # pre-render статической части фона (1 раз, не каждый кадр)
        self._static_bg = self._build_static_bg()
        # region-карта для dirty-detection
        # (ключ, x, y, w, h, value-fetcher)
        self._regions = [
            ("opencode", 100, 0,   280, 100,
             lambda s: (s.opencode.balance, s.opencode.plan,
                        int(time.time()) // 2)),
            ("cpu",      4,   100, 0,   140,
             lambda s: (round(s.cpu.usage), round(s.cpu.freq_ghz, 1),
                         s.cpu.temp_c, tuple(round(x) for x in (s.cpu.per_core or [])))),
            ("gpu",      0,   100, 0,   140,
             lambda s: (round(s.gpu.usage), s.gpu.temp_c,
                        round(s.gpu.vram_used_mb), round(s.gpu.vram_total_mb))),
            ("ram",      0,   100, 0,   140,
             lambda s: (round(s.mem.used_gb, 1), round(s.mem.total_gb),
                        round(s.mem.percent))),
            ("net",      0,   100, 0,   140,
             lambda s: (s.wifi.ssid, round(s.wifi.rx_bps / 1024),
                        round(s.wifi.tx_bps / 1024), s.wifi.signal_pct,
                        int(time.time()) // 2)),
        ]
        # ширины панелей (CPU|GPU|RAM|NET)
        self._mid_w = 0
        self._disk_w = 0

    def set_disks(self, disks: list[str]) -> None:
        self.disks = list(disks) if disks else ["C:\\"]
        # invalidate dirty cache
        self._last = {k: v for k, v in self._last.items()
                      if not k.startswith("disk_")}
        # rebuild static bg чтобы обновились рамки
        self._static_bg = self._build_static_bg()

    def set_network_type(self, kind: str) -> None:
        self.network_type = "ethernet" if kind == "ethernet" else "wifi"
        self._last.pop("net", None)
        self._static_bg = self._build_static_bg()

    def dirty_regions(self, snap) -> list:
        """Возвращает список (kind, key, x, y, w, h) регионов, которые изменились.
        GIF-углы — round-robin. Стат-панели — только при смене значения."""
        w, h = self.width, self.height
        n = 4
        gap = 4
        pw = (w - 2 * 4 - (n - 1) * gap) // n
        n2 = max(1, len(self.disks))
        pw2 = (380 - 4 - (n2 - 1) * gap) // n2

        out = []
        # GIF corners: round-robin
        gif_order = [("gastly",  0, 0),
                     ("haunter", w - GIF_SIZE, 0),
                     ("gengar",  w - GIF_SIZE, h - GIF_SIZE)]
        for i in range(3):
            idx = (self._gif_rr + i) % 3
            key, gx, gy = gif_order[idx]
            cur = self._frame_idx(key)
            if cur != self._gif_idx[key]:
                self._gif_idx[key] = cur
                out.append(("gif", key, gx, gy, GIF_SIZE, GIF_SIZE))
                self._gif_rr = (idx + 1) % 3
                break
        # обновим ширины CPU/GPU/RAM/NET при первом вызове
        if self._mid_w != pw:
            self._mid_w = pw
            self._regions[1] = ("cpu",  4,                       100, pw, 140, self._regions[1][5])
            self._regions[2] = ("gpu",  4 + (pw + gap),          100, pw, 140, self._regions[2][5])
            self._regions[3] = ("ram",  4 + 2 * (pw + gap),      100, pw, 140, self._regions[3][5])
            self._regions[4] = ("net",  4 + 3 * (pw + gap),      100, pw, 140, self._regions[4][5])
        # динамические диски: пересобираем _regions каждый раз при изменении кол-ва
        disks_regions = []
        for i, mount in enumerate(self.disks[:2]):
            key = "disk_" + (mount.rstrip(":\\") or "?").lower()
            x = 4 + i * (pw2 + gap)
            disks_regions.append((key, x, 240, pw2, 80,
                                  lambda s, m=mount: (round(_disk_pct(s, m)),)))
        # стат-панели (без дисков, они отдельно)
        for key, x, y, rw, rh, fetcher in self._regions[1:]:
            try:
                val = fetcher(snap)
            except Exception:
                val = None
            if val != self._last.get(key):
                self._last[key] = val
                out.append(("stat", key, x, y, rw, rh))
        # диски
        for key, x, y, rw, rh, fetcher in disks_regions:
            try:
                val = fetcher(snap)
            except Exception:
                val = None
            if val != self._last.get(key):
                self._last[key] = val
                out.append(("stat", key, x, y, rw, rh))
        return out

    def mark_sent(self, region):
        """Вызывается main-loop'ом после отправки региона.
        Обновляет tracking, чтобы в следующий dirty_regions() не было дублей."""
        kind = region[0]
        if kind == "gif":
            key = region[1]
            self._gif_idx[key] = self._frame_idx(key)

    def _placeholder(self, kind: str) -> Image.Image:
        s = GIF_SIZE
        im = Image.new("RGBA", (s, s), (0, 0, 0, 0))
        d = ImageDraw.Draw(im)
        cx, cy = s // 2, int(s * 0.55)
        r = int(s * 0.35)
        d.ellipse([cx - r, cy - r, cx + r, cy + r],
                  outline=(*NEON_P, 255), width=2)
        for dx, dy in [(-r - 6, -r - 14), (-r - 2, -r - 24),
                       (0, -r - 30), (r + 2, -r - 24), (r + 6, -r - 14)]:
            d.polygon([(cx + dx, cy + dy),
                       (cx + dx - 4, cy + dy + 12),
                       (cx + dx + 4, cy + dy + 12)], fill=(*NEON_P, 255))
        for sign in (-1, 1):
            ex = cx + sign * int(r * 0.4)
            ey = cy - int(r * 0.2)
            d.ellipse([ex - 5, ey - 5, ex + 5, ey + 5], fill=(*EYE_RED, 255))
        return im

    def _frame_idx(self, key: str) -> int:
        """Возвращает текущий индекс кадра GIF (0..len-1). 12 FPS внутри темы."""
        arr = self.neon.get(key, [])
        if len(arr) <= 1:
            return 0
        t = time.time() - self._t0
        return int(t * 12) % len(arr)

    def _frame(self, key: str):
        arr = self.neon.get(key, [])
        if not arr:
            return self._placeholder(key)
        idx = self._frame_idx(key)
        return arr[idx]

    def _build_static_bg(self) -> Image.Image:
        """Один раз рисует фон: градиент + glow + рамки панелей + рамки GIF-углов.
        Динамический контент (числа, бары, GIF-кадры) накладывается поверх в render()."""
        w, h = self.width, self.height
        bg = _vgrad(w, h, BG_TOP, BG_BOT).convert("RGBA")
        bg.alpha_composite(_radial_glow(w, h, w // 2, h + 40, 280, BG_MID, 220))
        d = ImageDraw.Draw(bg, "RGBA")
        # рамки панелей (CPU|GPU|RAM|NET) — 140px высота
        mid_y, mid_h = 100, 140
        n = 4; gap = 4
        pw = (w - 2 * 4 - (n - 1) * gap) // n
        net_lbl = "ETH" if self.network_type == "ethernet" else "WIFI"
        for i, lbl in enumerate(["CPU", "GPU", "RAM", net_lbl]):
            x = 4 + i * (pw + gap)
            _panel(d, x, mid_y, pw, mid_h, title=lbl, title_size=12)
        # рамки панелей дисков — 80px высота, по числу выбранных дисков (1 или 2)
        bot_y, bot_h = 240, 80
        n2 = max(1, len(self.disks))
        pw2 = (380 - 4 - (n2 - 1) * gap) // n2
        for i, mount in enumerate(self.disks[:2]):
            label = "DISK " + (mount.rstrip(":\\") or "?").upper()[:1]
            x = 4 + i * (pw2 + gap)
            _panel(d, x, bot_y, pw2, bot_h, title=label, title_size=12)
        # рамка OPENCODE
        _panel(d, 100, 0, 280, 100, title="OPENCODE", title_size=12)
        # декоративные углы GIF (белые уголки)
        for gx, gy in [(0, 0), (w - GIF_SIZE, 0),
                       (w - GIF_SIZE, h - GIF_SIZE)]:
            L = 8
            for x1, y1, x2, y2 in [
                (gx, gy, gx + L, gy), (gx, gy, gx, gy + L),
                (gx + GIF_SIZE, gy, gx + GIF_SIZE - L, gy),
                (gx + GIF_SIZE, gy, gx + GIF_SIZE, gy + L),
                (gx, gy + GIF_SIZE, gx + L, gy + GIF_SIZE),
                (gx, gy + GIF_SIZE, gx, gy + GIF_SIZE - L),
                (gx + GIF_SIZE, gy + GIF_SIZE, gx + GIF_SIZE - L, gy + GIF_SIZE),
                (gx + GIF_SIZE, gy + GIF_SIZE, gx + GIF_SIZE, gy + GIF_SIZE - L),
            ]:
                d.line([(x1, y1), (x2, y2)], fill=NEON_P2, width=2)
        return bg

    def render(self, snap: Snapshot) -> Image.Image:
        w, h = self.width, self.height
        # blit предкэшированного фона (вместо перерисовки градиента+glow+панелей)
        base = self._static_bg.copy()

        # === углы с GIF (только динамика) ===
        base.alpha_composite(self._frame("gastly"), (0, 0))
        base.alpha_composite(self._frame("haunter"), (w - GIF_SIZE, 0))
        base.alpha_composite(self._frame("gengar"), (w - GIF_SIZE, h - GIF_SIZE))

        d = ImageDraw.Draw(base, "RGBA")

        # === TOP MIDDLE: OPENCODE 280×100 ===
        self._draw_opencode_top(d, 100, 0, 280, 100, snap)

        # === MIDDLE ROW: 480×140 — CPU | GPU | RAM | NET ===
        mid_y, mid_h = 100, 140
        n = 4
        gap = 4
        pw = (w - 2 * 4 - (n - 1) * gap) // n
        self._draw_cpu(d, 4, mid_y, pw, mid_h, snap)
        self._draw_gpu(d, 4 + (pw + gap), mid_y, pw, mid_h, snap)
        self._draw_ram(d, 4 + 2 * (pw + gap), mid_y, pw, mid_h, snap)
        if self.network_type == "ethernet":
            self._draw_eth(d, 4 + 3 * (pw + gap), mid_y, pw, mid_h, snap)
        else:
            self._draw_wifi(d, 4 + 3 * (pw + gap), mid_y, pw, mid_h, snap)

        # === BOTTOM-LEFT: 380×80 — диски (1 или 2) ===
        bot_y, bot_h = 240, 80
        n2 = max(1, len(self.disks))
        pw2 = (380 - 4 - (n2 - 1) * gap) // n2
        for i, mount in enumerate(self.disks[:2]):
            x = 4 + i * (pw2 + gap)
            self._draw_disk(d, x, bot_y, pw2, bot_h, snap, mount=mount)

        return base.convert("RGB")

    # ---- TOP MIDDLE: OPENCODE ----
    def _draw_opencode_top(self, d, x, y, w, h, snap):
        oc = snap.opencode
        from datetime import datetime
        now = datetime.now()
        _panel(d, x, y, w, h, title="OPENCODE", title_size=12)
        # крупно баланс
        draw_text(d, (x + w // 2, y + 36), oc.balance, 24, NEON_C, bold=True, anchor="mm")
        if oc.plan:
            draw_text(d, (x + w - 10, y + 18), oc.plan, 9, NEON_P, anchor="ra")
        # время
        draw_text(d, (x + 10, y + h - 14),
                  now.strftime("%H:%M:%S"), 16, NEON_W, bold=True, anchor="lm")
        draw_text(d, (x + w - 10, y + h - 14),
                  now.strftime("%a %d.%m"), 11, NEON_P2, anchor="rm")

    def _draw_cores_sparkline(self, d, x, y, w, h, per_core):
        """Вертикальные мини-бары на каждое ядро. Цвет: зелёный→жёлтый→красный."""
        if not per_core:
            return
        n = len(per_core)
        gap = 1
        bw = max(2, (w - (n - 1) * gap) // n)
        for i, val in enumerate(per_core):
            val = max(0.0, min(100.0, val))
            bh = max(1, int(h * val / 100))
            bx = x + i * (bw + gap)
            by = y + h - bh
            if val < 60:
                col = (90, 230, 160)
            elif val < 85:
                col = (255, 220, 80)
            else:
                col = (255, 80, 80)
            d.rounded_rectangle([bx, by, bx + bw, y + h], radius=1, fill=col)

    # ---- CPU ----
    def _draw_cpu(self, d, x, y, w, h, snap):
        # большой % слева
        draw_text(d, (x + 8, y + 30),
                  f"{int(snap.cpu.usage)}%", 26, NEON_P, bold=True, anchor="lm")
        # имя процессора крупнее
        fnt = font("C:/Windows/Fonts/seguisb.ttf", 15)
        name = _trunc(d, snap.cpu.name, w // 2 - 6, fnt)
        draw_text(d, (x + 8, y + 50), name, 15, NEON_C, bold=True, anchor="lm")
        # частота / температура
        freq = f"{snap.cpu.freq_ghz:.1f}GHz" if snap.cpu.freq_ghz else "—"
        temp = f"{snap.cpu.temp_c:.0f}°" if snap.cpu.temp_c is not None else "—"
        draw_text(d, (x + 8, y + 68), freq, 15, NEON_C, bold=True)
        draw_text(d, (x + 80, y + 68), temp, 15, EYE_RED, bold=True, anchor="lm")
        # per-core sparkline (главная фича)
        cores = snap.cpu.per_core or []
        if cores:
            self._draw_cores_sparkline(d, x + 8, y + 80,
                                       w - 16, 36, cores)
        # статус-бар внизу
        _bar(d, x + 8, y + h - 18, w - 16, 10, snap.cpu.usage,
             fg=(180, 100, 255), bg=(40, 25, 60))

    # ---- GPU ----
    def _draw_gpu(self, d, x, y, w, h, snap):
        draw_text(d, (x + 8, y + 30),
                  f"{int(snap.gpu.usage)}%", 26, NEON_C, bold=True, anchor="lm")
        fnt = font("C:/Windows/Fonts/seguisb.ttf", 15)
        name = _trunc(d, snap.gpu.name, w // 2 - 6, fnt)
        draw_text(d, (x + 8, y + 50), name, 15, NEON_P, bold=True, anchor="lm")
        if snap.gpu.vram_total_mb > 0:
            vram = f"{snap.gpu.vram_used_mb/1024:.1f}/{snap.gpu.vram_total_mb/1024:.0f}G"
            draw_text(d, (x + 8, y + 68), vram, 15, NEON_C, bold=True)
        if snap.gpu.temp_c is not None:
            draw_text(d, (x + 80, y + 68), f"{snap.gpu.temp_c:.0f}°C",
                      15, EYE_RED, bold=True, anchor="lm")
        # VRAM bar (как per-core)
        if snap.gpu.vram_total_mb > 0:
            self._draw_vram_bar(d, x + 8, y + 80, w - 16, 36,
                                snap.gpu.vram_used_mb, snap.gpu.vram_total_mb)
        # статус-бар
        _bar(d, x + 8, y + h - 18, w - 16, 10, snap.gpu.usage,
             fg=(110, 230, 255), bg=(20, 30, 50))

    def _draw_vram_bar(self, d, x, y, w, h, used_mb, total_mb):
        """Большой бар VRAM с подписью GB внутри."""
        pct = (used_mb / total_mb * 100) if total_mb > 0 else 0
        d.rounded_rectangle([x, y, x + w, y + h], radius=4,
                            fill=(20, 20, 40), outline=(80, 80, 100), width=1)
        fw = max(1, int((w - 2) * pct / 100))
        d.rounded_rectangle([x + 1, y + 1, x + 1 + fw, y + h - 1],
                            radius=3, fill=(180, 100, 255))
        # текст внутри
        text = f"VRAM {used_mb/1024:.1f}/{total_mb/1024:.0f}G"
        draw_text(d, (x + w // 2, y + h // 2), text, 10,
                  (255, 255, 255), bold=True, anchor="mm")

    def _draw_mini_vram(self, d, x, y, w, h, used_mb, total_mb):
        pct = (used_mb / total_mb * 100) if total_mb > 0 else 0
        d.rounded_rectangle([x, y, x + w, y + h], radius=2,
                            fill=(30, 30, 50), outline=(80, 80, 100), width=1)
        fw = max(1, int((w - 2) * pct / 100))
        d.rounded_rectangle([x + 1, y + 1, x + 1 + fw, y + h - 1],
                            radius=1, fill=(180, 100, 255))

    # ---- RAM ----
    def _draw_ram(self, d, x, y, w, h, snap):
        used = snap.mem.used_gb
        total = snap.mem.total_gb
        draw_text(d, (x + w // 2, y + 32),
                  f"{used:.1f}G", 32, NEON_P, bold=True, anchor="mm")
        draw_text(d, (x + w // 2, y + 60),
                  f"of {total:.0f}G", 13, NEON_C, bold=True, anchor="mm")
        _bar(d, x + 8, y + h - 18, w - 16, 12, snap.mem.percent,
             fg=(180, 100, 255), bg=(40, 25, 60))

    # ---- WiFi ----
    def _draw_wifi(self, d, x, y, w, h, snap):
        rx = snap.wifi.rx_bps / (1024 * 1024)
        tx = snap.wifi.tx_bps / (1024 * 1024)
        fnt = font("C:/Windows/Fonts/seguisb.ttf", 15)
        ssid = _trunc(d, snap.wifi.ssid or "—", w - 12, fnt)
        draw_text(d, (x + w // 2, y + 32), ssid, 15, NEON_P, bold=True, anchor="mm")
        draw_text(d, (x + w // 2, y + 60),
                  f"↓{rx:.1f} M/s", 22, NEON_C, bold=True, anchor="mm")
        draw_text(d, (x + 8, y + 88),
                  f"↑{tx:.1f}M/s", 15, NEON_P2, bold=True)
        sig = snap.wifi.signal_pct
        draw_text(d, (x + w - 8, y + 88),
                  f"{sig}%", 15, EYE_RED, bold=True, anchor="ra")
        _bar(d, x + 8, y + h - 18, w - 16, 12, sig,
             fg=(110, 230, 255), bg=(20, 30, 50))

    def _draw_eth(self, d, x, y, w, h, snap):
        """Аналог WiFi-панели для Ethernet (без SSID, с именем интерфейса)."""
        rx = snap.wifi.rx_bps / (1024 * 1024)
        tx = snap.wifi.tx_bps / (1024 * 1024)
        fnt = font("C:/Windows/Fonts/seguisb.ttf", 15)
        iface = _trunc(d, snap.wifi.iface or "Ethernet", w - 12, fnt)
        draw_text(d, (x + w // 2, y + 32), iface, 15, NEON_P, bold=True, anchor="mm")
        draw_text(d, (x + w // 2, y + 60),
                  f"↓{rx:.1f} M/s", 22, NEON_C, bold=True, anchor="mm")
        draw_text(d, (x + 8, y + 88),
                  f"↑{tx:.1f}M/s", 15, NEON_P2, bold=True)
        draw_text(d, (x + w - 8, y + 88),
                  "100%", 15, EYE_RED, bold=True, anchor="ra")
        _bar(d, x + 8, y + h - 18, w - 16, 12, 100,
             fg=(110, 230, 255), bg=(20, 30, 50))

    # ---- DISK ----
    def _draw_disk(self, d, x, y, w, h, snap, mount):
        m = (mount or "").rstrip("/\\") + "\\"
        ds = None
        for k, v in snap.disks.items():
            if k.rstrip("/\\") + "\\" == m:
                ds = v; break
        if ds is None and snap.disk.mount.rstrip("/\\") + "\\" == m:
            ds = snap.disk
        if ds is None:
            draw_text(d, (x + w // 2, y + h // 2), "n/a", 18, NEON_C, anchor="mm")
            return
        # большой % сверху
        draw_text(d, (x + 8, y + 32),
                  f"{ds.percent:.0f}%", 24, NEON_P, bold=True, anchor="lm")
        # used/total крупнее (14pt)
        draw_text(d, (x + w - 8, y + 32),
                  f"{ds.used_gb:.0f}/{ds.total_gb:.0f}G", 14, NEON_C,
                  bold=True, anchor="ra")
        # статус-бар
        _bar(d, x + 8, y + h - 18, w - 16, 10, ds.percent,
             fg=(255, 220, 100), bg=(50, 40, 30))
