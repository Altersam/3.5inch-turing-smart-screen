"""Turing Smart Screen — протокол дисплея.

Команда: 6 байт = x(10) | y(10) | ex(10) | ey(10) | cmd(8)
Данные: поток RGB565 little-endian, чанки width*8 байт.

HELLO=69, CLEAR=102, RESET=101, SET_BRIGHTNESS=110,
SET_ORIENTATION=121, DISPLAY_BITMAP=197.
"""
from typing import Optional
import numpy as np
from PIL import Image


# Команды
HELLO           = 69     # 0x45
RESET           = 101    # 0x65
CLEAR           = 102    # 0x66
SCREEN_OFF      = 108    # 0x6C
SCREEN_ON       = 109    # 0x6D
SET_BRIGHTNESS  = 110    # 0x6E
SET_ORIENTATION = 121    # 0x79
DISPLAY_BITMAP  = 197    # 0xC5


def pack_cmd(x: int, y: int, ex: int, ey: int, cmd: int) -> bytes:
    b = bytearray(6)
    b[0] = (x >> 2) & 0xFF
    b[1] = (((x & 3) << 6) + (y >> 4)) & 0xFF
    b[2] = (((y & 15) << 4) + (ex >> 6)) & 0xFF
    b[3] = (((ex & 63) << 2) + (ey >> 8)) & 0xFF
    b[4] = (ey & 255) & 0xFF
    b[5] = cmd & 0xFF
    return bytes(b)


def image_to_rgb565_le(img: Image.Image) -> bytes:
    """PIL Image -> сырые байты RGB565 little-endian."""
    if img.mode != "RGB":
        img = img.convert("RGB")
    rgb = np.asarray(img)
    h, w = rgb.shape[0], rgb.shape[1]
    flat = rgb.reshape((w * h, 3)).astype(np.uint16)
    r = (flat[:, 0] >> 3) & 0x1F
    g = (flat[:, 1] >> 2) & 0x3F
    b = (flat[:, 2] >> 3) & 0x1F
    rgb565 = (r << 11) | (g << 5) | b
    return rgb565.astype("<u2").tobytes()


class TuringProtocol:
    """Готовит команды и кадры для отправки в сериал."""

    # Ориентации (совпадает с lib):
    #   0 = PORTRAIT         (320x480)
    #   1 = REVERSE_PORTRAIT (320x480)
    #   2 = LANDSCAPE        (480x320)
    #   3 = REVERSE_LANDSCAPE(480x320)
    def __init__(self, width: int = 320, height: int = 480,
                 orientation: int = 0):
        self.width = width
        self.height = height
        self.orientation = orientation

    def set_orientation(self, orientation: int) -> bytes:
        """orientation: 0..3. SetOrientation — 11 байт:
        6-байтный заголовок + (orientation+100) + 2 байта W + 2 байта H.
        Для landscape (2,3) W/H меняются на 480x320 — иначе дисплей
        думает, что буфер 320x480 и сдвигает картинку."""
        self.orientation = orientation
        if orientation in (2, 3):
            w, h = 480, 320
        else:
            w, h = 320, 480
        self.width, self.height = w, h
        header = pack_cmd(0, 0, 0, 0, SET_ORIENTATION)
        extra = bytes([(orientation + 100) & 0xFF,
                       (w >> 8) & 0xFF, w & 0xFF,
                       (h >> 8) & 0xFF, h & 0xFF])
        return header + extra

    def current_width(self) -> int:
        return 480 if self.orientation in (2, 3) else 320

    def hello(self) -> bytes:
        return bytes([HELLO] * 6)

    def reset(self) -> bytes:
        return pack_cmd(0, 0, 0, 0, RESET)

    def clear(self) -> bytes:
        return pack_cmd(0, 0, 0, 0, CLEAR)

    def screen_off(self) -> bytes:
        return pack_cmd(0, 0, 0, 0, SCREEN_OFF)

    def screen_on(self) -> bytes:
        return pack_cmd(0, 0, 0, 0, SCREEN_ON)

    def set_brightness(self, level: int) -> bytes:
        """level: 0..100. 0 = brightest, 100 = darkest."""
        v = max(0, min(255, int(255 * (100 - level) / 100)))
        return pack_cmd(v, 0, 0, 0, SET_BRIGHTNESS)

    def display_bitmap(self, image: Image.Image,
                       x: int = 0, y: int = 0) -> tuple[bytes, bytes]:
        """Возвращает (header, image_data)."""
        w, h = image.size
        ex, ey = x + w - 1, y + h - 1
        header = pack_cmd(x, y, ex, ey, DISPLAY_BITMAP)
        data = image_to_rgb565_le(image)
        return header, data

    def display_region(self, image: Image.Image,
                       x: int, y: int) -> tuple[bytes, bytes]:
        """Частичное обновление: image — это обрезанный кусок в координатах (x,y).
        Размеры региона берутся из image.size. Header содержит координаты."""
        w, h = image.size
        ex, ey = x + w - 1, y + h - 1
        header = pack_cmd(x, y, ex, ey, DISPLAY_BITMAP)
        data = image_to_rgb565_le(image)
        return header, data
