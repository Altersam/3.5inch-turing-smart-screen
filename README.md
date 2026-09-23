# Turing 3.5 Neon V38 - Fixed COM Hang Issues

[![Windows](https://img.shields.io/badge/Platform-Windows-blue.svg)](https://www.microsoft.com/windows)
[![Go Version](https://img.shields.io/badge/Go-1.23+-orange.svg)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

<div align="center">

## 🖥️ Turing 3.5 Neon Smart Screen Monitor V38 - Исправленная версия

Приложение для отображения показателей CPU, GPU, RAM, дисков в реальном времени на 3.5-дюймовом LCD дисплее с неоновой анимацией.

[![Version](https://img.shields.io/badge/version-V38-green.svg)](./README.md#🚀-быстрый-запуск)
[![Build Status](https://img.shields.io/badge/build-passing-success.svg)](./STATUS_REPORT.md)

</div>

---

## 🎯 Особенности V38 (исправления)

### 🔧 Исправления относительно V37:

| Проблема V37 | Решение в V38 |
|---------------|----------------|
| `FlushFileBuffers` блокировал процесс после 12+ минут работы | ✅ Убрано - данные передаются через `writeData()` с контролируемым таймаутом |
| Нет диагностики CTS перед отправкой данных | ✅ Добавлена функция `checkCTS()` для проверки сигнала готовности к приёму |
| RTS/CTS рукопожатие вызывало deadlock | ✅ Отключено (`fBinary, no RTS handshake`) |
| Нет контроля ошибок буферов | ✅ Добавлен `ClearCommError()` для проверки состояния очередей |
| При зависании переподключение не помогало | ✅ Улучшен reconnect с очисткой буферов и повторной проверкой CTS |
| Размер chunks 2048 байт вызывал проблемы | ✅ Уменьшён до 1024 байта для лучшей надёжности |

---

## 🚀 Быстрый запуск (готовый EXE)

### Вариант А: Готовое приложение (рекомендуется новичкам)

```powershell
# Скопируйте готовый EXE и запустите
cd C:\35inchENG\v2\Turing35NeonV38

# Запуск в тестовом режиме (без анимации)
.\Turing35NeonStableV38_temp.exe --test

# Или обычная версия с анимацией
.\Turing35NeonStableV38_temp.exe
```

### Вариант Б: Сборка из исходников

```powershell
cd C:\35inchENG\v2\3.5inch-turing-smart-screen-main

# Сборка исправленной версии
go build -ldflags="-s -w" -o Turing35NeonStableV38.exe main_v38.go

# Запуск
Turing35NeonStableV38.exe
```

---

## 📊 Режимы запуска

| Команда | Описание |
|---------|----------|
| `Turing35NeonStableV38.exe` | Обычная версия с неоновой анимацией |
| `Turing35NeonStableV38.exe --test` | Тестовый режим (без анимации) |
| `Turing35NeonStableV38.exe --preview` | Генерация превью изображения |

---

## 🔧 Системные требования

- **Windows 10/11** (64-bit)
- **Go 1.23+** (для сборки из исходников)
- **USB порт** с устройством CH340 (COM5 по умолчанию)
- **3.5-дюймовый LCD дисплей** с протоколом ST7796

---

## 📝 Установка

### 1. Из готовых файлов

Просто скопируйте файл EXE в нужную папку и запустите:
```powershell
.\Turing35NeonStableV38.exe
```

### 2. Из исходников

1. **Установите Go** (если не установлен):
   - Скачайте с [go.dev](https://go.dev/dl/)
   - Добавьте в PATH

2. **Соберите приложение**:
   ```powershell
   cd C:\35inchENG\v2\3.5inch-turing-smart-screen-main
   go build -ldflags="-s -w" -o Turing35NeonStableV38.exe main_v38.go
   ```

3. **Запустите**:
   ```powershell
   cd C:\35inchENG\v2\Turing35NeonV38
   .\Turing35NeonStableV38.exe --test
   ```

---

## 📖 Документация

| Файл | Описание |
|------|----------|
| [README.md](./README.md) | Основная документация |
| [STATUS_REPORT.md](./STATUS_REPORT.md) | Отчёт о статусе исправлений |
| [FINAL_INSTRUCTION.md](./FINAL_INSTRUCTION.md) | Итоговая инструкция |
| [REPORT_V38.md](./REPORT_V38.md) | Подробный отчёт об исправлениях |

---

## 🔍 Диагностика проблем

### Просмотр журнала в реальном времени:
```powershell
Get-Content .\app_v37.log -Tail 60 -Wait
```

**Ищите эти сообщения:**
- ✅ `COM connected=` — успешное подключение
- ✅ `COM write %d/%d bytes success` — успешная отправка данных
- ⚠️ `CTS not asserted` — проблема с сигналом готовности к приёму

---

## 🛠 Исправления V38

### Подробный список изменений:

1. **FlushFileBuffers блокировал процесс** ✅ Устранено
2. **Нет диагностики CTS перед отправкой данных** ✅ Устранено  
3. **RTS/CTS рукопожатие вызывало deadlock** ✅ Устранено
4. **Нет контроля ошибок буферов** ✅ Устранено
5. **При зависании переподключение не помогало** ✅ Устранено
6. **Размер chunks 2048 байт вызывал проблемы** ✅ Устранено

---

## 📦 Структура проекта

```
Turing35NeonV38-GitHub/
├── C:\35inchENG\v2\3.5inch-turing-smart-screen-main\
│   ├── main_v38.go                    # Исправленный исходный код V38
│   ├── build_v38.ps1                  # Скрипт сборки
│   └── assets/                        # Встроенные ресурсы
├── C:\35inchENG\v2\Turing35NeonV38\
│   ├── Turing35NeonStableV38_temp.exe # Готовый EXE из V37
│   └── README.md                      # Инструкция
└── docs/                              # Документация
    ├── README.md                      # Основная документация
    ├── STATUS_REPORT.md               # Отчёт о статусе
    ├── FINAL_INSTRUCTION.md           # Итоговая инструкция
    ├── REPORT_V38.md                  # Подробный отчёт об исправлениях
    └── install_go_v38.md              # Инструкция по установке Go

```

---

## 📞 Поддержка

Если приложение продолжает зависать:

1. **Проверьте журнал:** `app_v37.log` на наличие сообщений о CTS
2. **Переподключите USB-кабель:** Снимите и установите в Диспетчере устройств
3. **Попробуйте другой порт:** Используйте другой USB-порт или кабель
4. **Обновите драйвер CH340:** Скачайте последнюю версию с сайта TP-Link/Realtek

---

## ⚠️ Примечание

Приложение сохраняет полную функциональность:
- ✅ Реальное обновление баланса OpenCode
- ✅ Показ CPU/GPU/RAM/дисков в реальном времени
- ✅ Неоновая анимация (в обычной версии)
- ✅ Интеграция с системным трей Windows

Просто устранена проблема зависания COM через аппаратное рукопожатие RTS/CTS.

---

## 📄 Лицензия

MIT License - [see LICENSE file](./LICENSE)

---

## 🎉 Спасибо за использование!

<div align="center">
  <a href="https://github.com"><img src="https://img.shields.io/badge/GitHub-100000?style=for-the-badge&logo=github&logoColor=white" height="20"></a>
</div>
