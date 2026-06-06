"""Ручной сброс дисплея. Использовать когда дисплей завис.

  python reset_disp.py            # полный сброс (3 шага)
  python reset_disp.py --soft     # только software RESET (cmd=101)
  python reset_disp.py --dtr      # только DTR-пульс
  python reset_disp.py --clear    # только очистка экрана (без сброса)
  python reset_disp.py --port COM7  # конкретный порт
"""
import argparse
import sys
import time

from display.serial_lcd import SerialLCD


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", default="AUTO")
    ap.add_argument("--soft", action="store_true",
                    help="только software RESET (cmd=101) — быстро")
    ap.add_argument("--dtr", action="store_true",
                    help="только DTR-пульс (аппаратный reset CH340)")
    ap.add_argument("--clear", action="store_true",
                    help="только очистить экран (без сброса)")
    args = ap.parse_args()

    print(f"open SerialLCD on {args.port} ...")
    lcd = SerialLCD(port=args.port, auto_port=True)

    if not lcd.ser:
        print("ERROR: не удалось открыть порт. Проверь, что дисплей подключён.")
        return 1

    print("OK: дисплей открыт")

    if args.clear:
        print("→ очистка экрана...")
        ok = lcd.clear()
        print("OK" if ok else "FAIL")
        lcd.close()
        return 0 if ok else 1

    if args.soft:
        print("→ software RESET (cmd=101)...")
        if lcd.ser and hasattr(lcd, "_tp"):
            try:
                lcd.ser.write(lcd._tp.reset())
                lcd.ser.flush()
                print("отправлено. Ждём 5 сек...")
                lcd._safe_close()
                time.sleep(5.0)
                lcd.port = lcd._find_port() or lcd.port
                lcd._open()
                print("OK" if lcd.ser else "FAIL: дисплей не ожил")
            except Exception as e:
                print(f"FAIL: {e}")
        lcd.close()
        return 0 if lcd.ser else 1

    if args.dtr:
        print("→ DTR pulse...")
        import serial
        # обязательно закрыть текущий порт иначе PermissionError
        lcd.close()
        time.sleep(0.5)
        try:
            s = serial.Serial(lcd.port, lcd.baudrate,
                              timeout=0.5, write_timeout=2.0, rtscts=False)
            s.dtr = False
            time.sleep(0.1)
            s.dtr = True
            s.close()
            time.sleep(3.0)
            lcd.port = lcd._find_port() or lcd.port
            lcd._open()
            print("OK" if lcd.ser else "FAIL")
        except Exception as e:
            print(f"FAIL: {e}")
        lcd.close()
        return 0 if lcd.ser else 1

    # full reset (default)
    print("→ полный сброс (3 шага)...")
    ok = lcd.reset()
    if ok:
        print("OK: дисплей ожил")
        lcd.close()
        return 0
    print("FAIL: дисплей не ожил. Попробуй:")
    print("  - python reset_disp.py --dtr")
    print("  - вытащи-вставь USB (1+ минута)")
    lcd.close()
    return 1


if __name__ == "__main__":
    sys.exit(main())
