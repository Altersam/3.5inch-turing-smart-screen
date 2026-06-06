"""Окно предпросмотра на ПК (через tkinter). Показывает тот же кадр,
что и физический дисплей, в масштабированном окне."""
import time
import threading
import queue
import tkinter as tk
from PIL import Image, ImageTk

from .base import BaseDisplay


class PreviewWindow(BaseDisplay):
    def __init__(self, width: int = 480, height: int = 320, scale: int = 2):
        self.width = width
        self.height = height
        self.scale = scale
        self._q = queue.Queue(maxsize=1)
        self._stop = threading.Event()
        self._root = None
        self._canvas = None
        self._img_tk = None
        self._thread = threading.Thread(target=self._tk_main, daemon=True)
        self._thread.start()
        # ждём, пока окно создастся
        for _ in range(50):
            if self._root is not None:
                break
            time.sleep(0.05)

    def _tk_main(self):
        try:
            self._root = tk.Tk()
            self._root.title("UsbDisplay — preview")
            self._root.configure(bg="#0a0a0a")
            w, h = self.width * self.scale, self.height * self.scale
            self._root.geometry(f"{w}x{h}+100+100")
            self._canvas = tk.Canvas(self._root, width=w, height=h,
                                     bg="#000", highlightthickness=0)
            self._canvas.pack()
            self._root.protocol("WM_DELETE_WINDOW", self._on_close)
            self._tick()
            self._root.mainloop()
        except Exception as e:
            print(f"preview init failed: {e}")

    def _tick(self):
        if self._stop.is_set():
            return
        try:
            img = self._q.get_nowait()
            big = img.resize((self.width * self.scale, self.height * self.scale),
                             Image.NEAREST)
            self._img_tk = ImageTk.PhotoImage(big)
            self._canvas.create_image(0, 0, anchor="nw", image=self._img_tk)
        except queue.Empty:
            pass
        if self._root:
            self._root.after(50, self._tick)

    def _on_close(self):
        self._stop.set()
        if self._root:
            self._root.destroy()

    def send_frame(self, image: Image.Image) -> None:
        if image.size != (self.width, self.height):
            image = image.resize((self.width, self.height), Image.LANCZOS)
        try:
            self._q.put_nowait(image.copy())
        except queue.Full:
            try:
                self._q.get_nowait()
                self._q.put_nowait(image.copy())
            except Exception:
                pass

    def close(self) -> None:
        self._stop.set()
        if self._root:
            try:
                self._root.after(0, self._root.destroy)
            except Exception:
                pass
