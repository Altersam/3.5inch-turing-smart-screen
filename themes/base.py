"""Базовый класс темы. Тема получает снимок железа и рисует PIL Image."""
from __future__ import annotations
from abc import ABC, abstractmethod
from PIL import Image

from sensors.hardware import Snapshot


class BaseTheme(ABC):
    width: int = 480
    height: int = 320

    def __init__(self, width: int, height: int, orientation: str = "landscape"):
        self.width = width
        self.height = height
        self.orientation = orientation

    @abstractmethod
    def render(self, snap: Snapshot) -> Image.Image: ...
