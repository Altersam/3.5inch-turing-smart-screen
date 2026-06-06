"""Рендер дисплея в стиле «фото устройства» — с рамкой, тенью, логотипом.

Использует NeonGengarTheme чтобы получить настоящий кадр 480×320,
потом оборачивает его в «корпус» 3.5" дисплея и масштабирует.

Запуск:
    python make_display_render.py [--out display_render.png]
"""
from __future__ import annotations
import argparse
import sys
import time
import random
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from core.config import Config
from sensors.hardware import HardwareMonitor
from themes.neon_gengar import NeonGengarTheme


# -------- демо-данные, чтобы скриншот выглядел красиво --------
def _pretty_snapshot():
    """Снимок с красивыми цифрами — для рекламного рендера."""
    snap = HardwareMonitor.Snapshot.__new__(HardwareMonitor.Snapshot)
    # CPU
    snap.cpu_percent = 27.0
    snap.cpu_freq_ghz = 4.2
    snap.cpu_temp_c = 52.0
    snap.per_core = [12, 87, 33, 56, 23, 41, 67, 19, 28, 44, 73, 31, 55, 18, 22]
    snap.cpu_history = [
        [random.randint(15, 70) for _ in range(20)] for _ in range(15)
    ]
    # GPU
    snap.gpu_percent = 38.0
    snap.gpu_temp_c = 64.0
    snap.gpu_mem_used = 3.2
    snap.gpu_mem_total = 16.0
    snap.vram_percent = 20.0
    # RAM
    snap.ram_used = 12.4
    snap.ram_total = 32.0
    snap.ram_percent = 38.7
    # Network
    snap.net_rx = 1.4
    snap.net_tx = 0.32
    snap.net_rx_mb = 184.2
    snap.net_tx_mb = 47.6
    snap.net_iface = "Wi-Fi"
    snap.net_quality = 94
    # Disks
    snap.disk_c_used = 234.5
    snap.disk_c_total = 931.0
    snap.disk_c_percent = 25.2
    snap.disk_z_used = 649.4
    snap.disk_z_total = 954.0
    snap.disk_z_percent = 68.0
    # OpenCode
    snap.opencode_balance = "$ 1.43"
    snap.opencode_status = "live"
    # Misc
    snap.timestamp = time.time()
    return snap


# -------- рендер в фото-стиле --------
def _make_device_frame(display_img: Image.Image,
                       scale: int = 3) -> Image.Image:
    """Оборачивает 480×320 кадр в «корпус» дисплея 3.5" с тенью."""
    W, H = display_img.size
    # 1) чёткое масштабирование
    inner = display_img.resize((W * scale, H * scale), Image.NEAREST)

    # 2) рамка вокруг экрана
    bezel = 26 * scale
    fw, fh = inner.size[0] + bezel * 2, inner.size[1] + bezel * 2
    frame = Image.new("RGB", (fw, fh), (8, 8, 12))
    d = ImageDraw.Draw(frame)

    # subtle bevel
    for i in range(bezel):
        c = 8 + int((1 - i / bezel) * 6)
        d.rectangle([i, i, fw - i - 1, fh - i - 1],
                    outline=(c, c, c + 4), width=1)

    # вклеиваем экран
    frame.paste(inner, (bezel, bezel))

    # маленький «светодиод» снизу
    led_r = 4 * scale
    led_x = fw // 2
    led_y = fh - bezel // 2
    d.ellipse([led_x - led_r, led_y - led_r,
               led_x + led_r, led_y + led_r],
              fill=(110, 230, 255))
    d.ellipse([led_x - led_r // 2, led_y - led_r // 2,
               led_x + led_r // 2, led_y + led_r // 2],
              fill=(220, 250, 255))

    # логотип «CH340 / ST7796» сверху
    try:
        font = ImageFont.truetype("arial.ttf", 11 * scale)
    except OSError:
        font = ImageFont.load_default()
    label = "USB 3.5\"  TURING  480×320"
    bbox = d.textbbox((0, 0), label, font=font)
    tw = bbox[2] - bbox[0]
    d.text(((fw - tw) // 2, 4 * scale), label,
           fill=(70, 70, 90), font=font)

    # 3) тень под устройством
    shadow_pad = 30 * scale
    canvas_w = fw + shadow_pad * 2
    canvas_h = fh + shadow_pad * 2
    canvas = Image.new("RGBA", (canvas_w, canvas_h), (0, 0, 0, 0))

    shadow = Image.new("L", (fw, fh), 0)
    sd = ImageDraw.Draw(shadow)
    sd.rectangle([0, 0, fw, fh], fill=180)
    shadow = shadow.filter(ImageFilter.GaussianBlur(28 * scale // 3))
    canvas.paste((0, 0, 0), (shadow_pad, shadow_pad + 18 * scale), shadow)

    canvas.paste(frame, (shadow_pad, shadow_pad))
    return canvas


# -------- рендер сцены: ноутбук/монитор + дисплей-герой --------
def _make_hero(canvas: Image.Image,
               title: str = "USB 3.5\" Turing Smart Screen") -> Image.Image:
    """Сцена с заголовком и подписью под устройством."""
    cw, ch = canvas.size
    pad_x = 60
    pad_y = 110
    title_h = 90
    sub_h = 60
    bg_w = cw + pad_x * 2
    bg_h = ch + pad_y * 2 + title_h + sub_h

    bg = Image.new("RGB", (bg_w, bg_h), (12, 8, 24))
    bd = ImageDraw.Draw(bg)

    # subtle gradient
    for y in range(bg_h):
        t = y / bg_h
        r = int(12 + 18 * t)
        g = int(8 + 6 * t)
        b = int(24 + 40 * t)
        bd.line([(0, y), (bg_w, y)], fill=(r, g, b))

    # title
    try:
        title_font = ImageFont.truetype("arialbd.ttf", 64)
        sub_font = ImageFont.truetype("arial.ttf", 28)
        badge_font = ImageFont.truetype("consola.ttf", 22)
    except OSError:
        title_font = ImageFont.load_default()
        sub_font = title_font
        badge_font = title_font

    bb = bd.textbbox((0, 0), title, font=title_font)
    tw = bb[2] - bb[0]
    bd.text(((bg_w - tw) // 2, 30), title,
            fill=(225, 90, 255), font=title_font)

    subtitle = "480×320  •  ST7796  •  CH340  •  Python + Pillow"
    sb = bd.textbbox((0, 0), subtitle, font=sub_font)
    sw = sb[2] - sb[0]
    bd.text(((bg_w - sw) // 2, 30 + 80), subtitle,
            fill=(180, 200, 255), font=sub_font)

    # device
    bg.paste(canvas, (pad_x, pad_y), canvas if canvas.mode == "RGBA" else None)

    # badge / chip
    chip = "OPENCODE  •  CPU  •  GPU  •  RAM  •  NET  •  DISK"
    cb = bd.textbbox((0, 0), chip, font=badge_font)
    cw_chip = cb[2] - cb[0] + 60
    ch_chip = cb[3] - cb[1] + 30
    cx = (bg_w - cw_chip) // 2
    cy = pad_y + ch + 24
    bd.rounded_rectangle([cx, cy, cx + cw_chip, cy + ch_chip],
                         radius=18, fill=(40, 18, 70),
                         outline=(225, 90, 255), width=2)
    bd.text((cx + 30, cy + 12), chip,
            fill=(225, 230, 255), font=badge_font)

    return bg


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", type=Path,
                    default=HERE / "screenshot_display.png")
    ap.add_argument("--scale", type=int, default=3)
    ap.add_argument("--no-frame", action="store_true",
                    help="только экран, без рамки/тени")
    ap.add_argument("--no-hero", action="store_true",
                    help="без сцены (только экран/рамка)")
    args = ap.parse_args()

    cfg = Config.load()
    cfg.ui.theme = "neon_gengar"

    snap = _pretty_snapshot()
    theme = NeonGengarTheme(
        cfg.display.width, cfg.display.height,
        cfg.display.orientation,
        gif_dir=str(HERE / "gif"),
        disks=["C:\\", "Z:\\"],
        network_type="wifi",
    )
    img = theme.render(snap)
    img.save(HERE / "screenshot_display_raw.png")

    if args.no_frame:
        out = img
    else:
        framed = _make_device_frame(img, scale=args.scale)
        if args.no_hero:
            out = framed
        else:
            out = _make_hero(framed)

    out.save(args.out)
    print(f"saved {args.out}  size={out.size}")


if __name__ == "__main__":
    main()
