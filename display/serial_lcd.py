"""Драйвер дисплея через COM-порт (pyserial). Использует протокол Turing."""
import time
from typing import Optional

from PIL import Image

from .base import BaseDisplay
from protocol import turing
from core.logger import get_logger

log = get_logger("display.serial")


class SerialLCD(BaseDisplay):
    def __init__(self, port: str, baudrate: int = 115200,
                 width: int = 480, height: int = 320,
                 orientation: str = "landscape",
                 framebuffer_addr: int = 0x001000,
                 brightness: int = 100,
                 auto_port: bool = True,
                 force_orient: int | None = None,
                 force_rotate: int = 0):
        self.port = port
        self.baudrate = baudrate
        self.width = width
        self.height = height
        self.orientation = orientation
        self.brightness = brightness
        self.force_orient = force_orient
        self.force_rotate = force_rotate
        self.ser = None
        self._consecutive_errors = 0
        self.ori_val = 2  # physical orient (2=landscape по умолчанию)
        # physical display always 320x480 portrait
        self.disp_w = 320
        self.disp_h = 480
        if auto_port and port.upper() in ("AUTO", ""):
            self.port = self._find_port() or port
        self._open()

    @staticmethod
    def _find_port():
        try:
            import serial.tools.list_ports
        except Exception:
            return None
        for p in serial.tools.list_ports.comports():
            vid = p.vid or 0
            if vid in (0x1A86, 0x10C4, 0x0403, 0x067B, 0x2341, 0x239A):
                log.info("auto-detected port: %s (vid=%04X)", p.device, vid)
                return p.device
        return None

    def _open(self):
        try:
            import serial
            log.info("opening %s @ %d baud (rtscts=True)",
                     self.port, self.baudrate)
            self.ser = serial.Serial(
                port=self.port,
                baudrate=self.baudrate,
                bytesize=serial.EIGHTBITS,
                parity=serial.PARITY_NONE,
                stopbits=serial.STOPBITS_ONE,
                timeout=0.5,
                write_timeout=5.0,
                rtscts=True,
                # xonxoff=False, rtscts=True — как в оригинале
            )
            self.ser.reset_input_buffer()
            self.ser.reset_output_buffer()
            self._init()
            log.info("display ready")
        except Exception as e:
            log.error("failed to open display: %s", e)
            self.ser = None

    def _init(self):
        if not self.ser:
            return
        tp = turing.TuringProtocol(self.disp_w, self.disp_h)
        try:
            self.ser.reset_input_buffer()
            self.ser.reset_output_buffer()
        except Exception:
            pass
        # hello
        log.info("init: hello")
        try:
            self.ser.write(tp.hello())
            self.ser.flush()
        except Exception as e:
            log.warning("hello write failed: %s", e)
        time.sleep(0.1)
        try:
            self.ser.timeout = 0.2
            resp = self.ser.read(6)
            if resp and len(resp) >= 6:
                if resp[0] == 0x01:
                    log.info("sub-rev: USBMONITOR_3_5")
                elif resp[0] == 0x02:
                    log.info("sub-rev: USBMONITOR_5")
                elif resp[0] == 0x03:
                    log.info("sub-rev: USBMONITOR_7")
                else:
                    log.info("sub-rev unknown: %s", resp.hex())
            else:
                log.info("no HELLO response → TURING_3_5 / standard")
        except Exception as e:
            log.info("hello read error: %s", e)
        self.ser.timeout = 0.5
        # orientation
        if self.force_orient is not None:
            ori = self.force_orient
        else:
            ori = 2 if self.orientation == "landscape" else 0
        self.ori_val = ori
        log.info("init: set_orientation(%d)", ori)
        try:
            self.ser.write(tp.set_orientation(ori))
            self.ser.flush()
        except Exception as e:
            log.warning("set_orientation failed: %s", e)
        time.sleep(0.2)
        # clear (с текущей ориентацией, без танца portrait→clear→restore)
        try:
            self.ser.write(tp.clear())
            self.ser.flush()
        except Exception as e:
            log.warning("clear failed: %s", e)
        time.sleep(0.3)
        # brightness
        try:
            self.ser.write(tp.set_brightness(self.brightness))
            self.ser.flush()
        except Exception as e:
            log.warning("set_brightness failed: %s", e)
        time.sleep(0.1)
        self._tp = tp
        self._cur_ori = ori

    def _prepare_image(self, image: Image.Image) -> Image.Image:
        """Приводит image к физическому размеру дисплея (с учётом rotate)."""
        ori = getattr(self, "_cur_ori", 2)
        phys_w, phys_h = (self.disp_h, self.disp_w) if ori in (2, 3) \
            else (self.disp_w, self.disp_h)
        if image.size != (phys_w, phys_h):
            image = image.resize((phys_w, phys_h), Image.LANCZOS)
        if self.force_rotate:
            image = image.rotate(self.force_rotate, expand=True)
            if image.size != (phys_w, phys_h):
                image = image.resize((phys_w, phys_h), Image.LANCZOS)
        return image

    def send_frame(self, image: Image.Image) -> None:
        if not self.ser:
            return
        try:
            image = self._prepare_image(image)
            tp = getattr(self, "_tp", None) or turing.TuringProtocol(self.disp_w, self.disp_h)
            tp._current_orient = getattr(self, "_cur_ori", 2)
            header, data = tp.display_bitmap(image, 0, 0)
            self.ser.write(header)
            self.ser.flush()
            chunk = tp.current_width() * 8
            for i in range(0, len(data), chunk):
                self.ser.write(data[i:i + chunk])
            self.ser.flush()
            self._consecutive_errors = 0
        except Exception as e:
            log.error("send_frame failed: %s", e)
            self._consecutive_errors += 1
            if self._consecutive_errors > 10:
                log.warning("display stuck — software reset (cmd=101)")
                self.reset()
                self._consecutive_errors = 0

    def send_region(self, image: Image.Image, x: int, y: int,
                    w: int, h: int) -> None:
        """Partial update: отправляет только регион (x,y,w,h) из image.
        Это в РАЗЫ быстрее полного кадра на медленном baud."""
        if not self.ser:
            return
        try:
            image = self._prepare_image(image)
            region = image.crop((x, y, x + w, y + h))
            tp = getattr(self, "_tp", None) or turing.TuringProtocol(self.disp_w, self.disp_h)
            tp._current_orient = getattr(self, "_cur_ori", 2)
            header, data = tp.display_region(region, x, y)
            self.ser.write(header)
            self.ser.flush()
            cw = tp.current_width() * 8
            chunk = min(cw, len(data) or cw)
            for i in range(0, len(data), chunk):
                self.ser.write(data[i:i + chunk])
            self.ser.flush()
            self._consecutive_errors = 0
        except Exception as e:
            log.error("send_region failed: %s", e)
            self._consecutive_errors += 1
            if self._consecutive_errors > 10:
                log.warning("display stuck — software reset (cmd=101)")
                self.reset()
                self._consecutive_errors = 0

    def reset(self) -> bool:
        """Многоступенчатый сброс дисплея.

        Шаги (каждый следующий — fallback если предыдущий не помог):
          1) Software RESET (cmd=101) + close + 5s wait + reopen
          2) DTR-пульс (аппаратный reset CH340) + reopen
          3) Полная ре-инициализация pyserial + ждать USB re-enumerate
        Возвращает True если дисплей ожил."""
        log.warning("=== RESET display (%s) ===", self.port)
        old_port = self.port

        # --- шаг 1: software reset ---
        if self.ser:
            try:
                self.ser.write(self._tp.reset() if hasattr(self, "_tp")
                               else b"")
                self.ser.flush()
                log.info("RESET cmd sent")
            except Exception as e:
                log.warning("step1: RESET write failed (expected if stuck): %s", e)
        self._safe_close()
        time.sleep(5.0)
        # переискать порт
        new_port = self._find_port() or old_port
        if new_port != old_port:
            log.info("port changed: %s → %s", old_port, new_port)
        self.port = new_port
        self._open()
        if self.ser:
            log.info("step1 OK: display recovered via software RESET")
            return True

        # --- шаг 2: DTR-пульс (аппаратный reset CH340) ---
        log.info("step2: trying DTR pulse reset")
        try:
            import serial
            s = serial.Serial(self.port, self.baudrate, timeout=0.5,
                              write_timeout=2.0, rtscts=False)
            # DTR low → 50ms → high = аппаратный reset на CH340
            s.dtr = False
            time.sleep(0.05)
            s.dtr = True
            time.sleep(0.05)
            s.close()
            time.sleep(3.0)
        except Exception as e:
            log.warning("step2: DTR pulse failed: %s", e)
        self.port = self._find_port() or old_port
        self._open()
        if self.ser:
            log.info("step2 OK: display recovered via DTR pulse")
            return True

        # --- шаг 3: долгое ожидание USB re-enumerate ---
        log.info("step3: waiting 10s for USB re-enumeration")
        time.sleep(10.0)
        self.port = self._find_port() or old_port
        self._open()
        if self.ser:
            log.info("step3 OK: display recovered after long wait")
            return True

        log.error("all reset steps failed — physical unplug-replug required")
        return False

    def clear(self) -> bool:
        """Просто очищает экран (белый) — без перезагрузки."""
        if not self.ser or not hasattr(self, "_tp"):
            return False
        try:
            self.ser.write(self._tp.clear())
            self.ser.flush()
            log.info("display cleared")
            return True
        except Exception as e:
            log.error("clear failed: %s", e)
            return False

    def _safe_close(self):
        if self.ser:
            try:
                self.ser.close()
            except Exception:
                pass
            self.ser = None

    def close(self) -> None:
        self._safe_close()
