# UsbDisplay — Прогресс

## Цель
Python-приложение для 3.5" USB Turing Smart Screen (CH340, ST7796), 480×320, тема "neon gengar" с 3 GIF покемонов в углах.

## Стек
- Python 3 + Pillow + pyserial + numpy + psutil + pynvml + wmi + pywin32
- Рабочая папка: `Z:\35inchENG\UsbDisplay`

## Протокол Turing
- 6-байт заголовок (x10|y10|ex10|ey10|cmd8), CH340
- **115200 baud надёжно; 230400 работает; 460800/921600 — шумят/виснут**
- rtscts=True, RGB565 LE, chunks = current_width*8 байт
- Команды: HELLO=69, CLEAR=102, RESET=101, SET_BRIGHTNESS=110, SET_ORIENTATION=121, DISPLAY_BITMAP=197
- Ориентация: 2 (landscape, 480×320), rotate=0 по умолчанию

## Структура файлов
- `main.py` — точка входа, флаги --reset, --no-preview, --rotate, --theme
- `themes/neon_gengar.py` — основная тема
- `themes/{gengar,minimal,cyberpunk}.py` — другие темы
- `sensors/hardware.py` — CpuSample.per_core, DiskSample per-mount Dict, WifiSample, OpenCodeSample
- `display/serial_lcd.py` — SerialLCD с reset() (3-step), send_frame, send_region
- `protocol/turing.py` — TuringProtocol с display_bitmap, display_region
- `reset_disp.py` — --soft/--dtr/--clear
- `gif/{gastly,haunter,gengar}.gif`
- `opencode_cookie.txt`, `opencode_token.txt`
- `core/config.py` — DisplayConfig (port, baudrate=230400, w=480, h=320), UIConfig (framerate=5)

## OpenCode
- URL: workspace `wrk_01KK7F1MHNJN72MSQH0A8YFXVF`
- Cookie в `opencode_cookie.txt` (имя cookie: `auth`)
- Кэш 60с в `HardwareMonitor._opencode_sample()`
- Unit: raw/100000000 = $ (e.g. 8042954304 = $80.43)

## Layout (480×320, neon_gengar)
- Gastly 100×100 TL (0,0)
- Haunter 100×100 TR (380,0)
- Gengar 100×100 BR (380,220)
- OPENCODE 280×100 top-middle (100,0)
- Middle row 480×140 (y=100-240): CPU|GPU|RAM|WIFI
- Bottom row 380×80 (y=240-320): DISK C | DISK Z
- Мелкий текст 15pt; заголовки 12pt; большие числа 24-32pt

## Partial Updates
- `send_region(image, x, y, w, h)` в SerialLCD + protocol
- `dirty_regions(snap)` в теме: round-robin GIF + все изменённые статы
- GIF-кадры перебираются по очереди: gastly → haunter → gengar → ...
- mark_sent обновляет tracking после отправки
- Первый кадр — ПОЛНЫЙ, чтобы очистить мусор в буфере дисплея

## FPS реальность
- 480×320 full frame = 307KB = 13.4с @ 230400 / 26.7с @ 115200
- 100×100 region = 20KB = 0.87с @ 230400 / 1.74с @ 115200
- Цикл 3 углов GIF: 3 тика по ~0.87с = ~2.6с @ 230400

## Команды
- Запуск: `cd /d Z:\35inchENG\UsbDisplay && python -u main.py --reset --theme neon_gengar --no-preview --rotate 0`
- Reset: `python reset_disp.py --dtr` или `--soft` или полный

## TODO (следующие шаги)
- [ ] GUI для ввода OpenCode (cookie/token)
- [ ] Выбор 1 или 2 дисков
- [ ] Выбор WiFi / Ethernet
- [ ] Кнопка "Запустить экран"
- [ ] Сворачивание в системный трей
- [ ] Автозапуск при загрузке Windows
