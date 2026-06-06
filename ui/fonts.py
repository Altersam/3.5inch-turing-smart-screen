"""Кеш TTF-шрифтов. get(path, size) → ImageFont."""
from __future__ import annotations
from functools import lru_cache
from PIL import ImageFont


@lru_cache(maxsize=64)
def get(path: str, size: int):
    """Загрузить TTF-шрифт. size — в pt."""
    try:
        return ImageFont.truetype(path, size)
    except (OSError, FileNotFoundError):
        return ImageFont.load_default()
