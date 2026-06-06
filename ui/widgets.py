"""Виджеты для рендера на PIL Image.

text()  — рисование текста с тенью/обводкой/anchor.
bar()   — горизонтальный/вертикальный прогресс-бар.
ring()  — круговая шкала.
vbar()  — вертикальный бар (как в старых темах).
"""
from __future__ import annotations
from PIL import ImageDraw, ImageFont
from .fonts import get as font


def text(d: ImageDraw.ImageDraw, pos, text_str, size, color,
         bold: bool = False, anchor: str = "la",
         shadow: bool = False, shadow_color=(0, 0, 0),
         font_path: str = "C:/Windows/Fonts/seguisb.ttf"):
    """Универсальный вывод текста.

    pos: (x, y)
    size: pt
    color: (r, g, b)
    anchor: lt/ct/rt/lm/cm/rm/lb/cb/rb (PIL anchor)
    shadow: рисовать ли чёрную тень
    """
    f = font(font_path, size)
    if shadow:
        x, y = pos
        if anchor and len(anchor) == 2:
            ax, ay = anchor
            if ax == "l":
                sx = x + 1
            elif ax == "r":
                sx = x - 1
            else:
                sx = x
            sy = y + 1
            d.text((sx, sy), text_str, font=f, fill=shadow_color, anchor=anchor)
    d.text(pos, text_str, font=f, fill=color, anchor=anchor)


def bar(d: ImageDraw.ImageDraw, x, y, w, h, pct, color,
        bg=(40, 20, 70), outline=None, fill_bg: bool = True):
    """Горизонтальный бар. pct 0..100."""
    if fill_bg:
        d.rectangle([x, y, x + w, y + h], fill=bg)
    if outline is not None:
        d.rectangle([x, y, x + w, y + h], outline=outline, width=1)
    fw = max(0, min(w - 2, int((w - 2) * pct / 100.0)))
    d.rectangle([x + 1, y + 1, x + 1 + fw, y + h - 1], fill=color)


def vbar(d: ImageDraw.ImageDraw, x, y, w, h, pct, color,
         bg=(40, 20, 70), outline=None):
    """Вертикальный бар."""
    if outline is None:
        d.rectangle([x, y, x + w, y + h], fill=bg)
    else:
        d.rectangle([x, y, x + w, y + h], fill=bg, outline=outline, width=1)
    fh = max(0, min(h - 2, int((h - 2) * pct / 100.0)))
    d.rectangle([x + 1, y + h - 1 - fh, x + w - 1, y + h - 1], fill=color)


def ring(d: ImageDraw.ImageDraw, cx, cy, r, pct, color,
         width=4, bg=(40, 20, 70)):
    """Круговая шкала 0..100."""
    d.ellipse([cx - r, cy - r, cx + r, cy + r], outline=bg, width=width)
    if pct <= 0:
        return
    import math
    end_a = -math.pi / 2 + 2 * math.pi * pct / 100.0
    d.arc([cx - r, cy - r, cx + r, cy + r], -90,
          int(math.degrees(end_a)), fill=color, width=width)
