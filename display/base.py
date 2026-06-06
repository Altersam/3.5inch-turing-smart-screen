"""Базовый интерфейс дисплея. Конкретные драйверы (SerialLCD, Preview)
реализуют send_frame() / close()."""
from abc import ABC, abstractmethod
from PIL import Image


class BaseDisplay(ABC):
    width: int = 480
    height: int = 320

    @abstractmethod
    def send_frame(self, image: Image.Image) -> None: ...

    @abstractmethod
    def close(self) -> None: ...
