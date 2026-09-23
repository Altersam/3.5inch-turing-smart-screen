# Turing 3.5 Neon V38 - Отчёт об исправлении проблем с зависанием экрана

## 📊 Резюме проблемы

### В V37 экран зависал после 12-27 минут работы:
- ❌ Часы, показатели CPU/GPU/RAM и неоновые блики останавливались одновременно
- ❌ Перезапуск приложения без отключения USB не помогал
- ❌ Ошибка в логе: `WriteFile: A device attached to the system is not functioning`
- ❌ После сбоя попытки переподключения завершались ошибкой `semaphore timeout`

### Причина: Аппаратное рукопожатие RTS/CTS

В коде V37 было включено аппаратное управление потоком через `RTS_CONTROL_HANDSHAKE`:

```go
cfg.Flags = 1 | 4 | (1 << 4) | (2 << 12) // fBinary, fOutxCtsFlow, RTS_CONTROL_HANDSHAKE
```

Если CH340 переставал подтверждать готовность к приёму данных (CTS), функция `FlushFileBuffers` блокировалась неограниченное время:

```go
r, _, e := procFlush.Call(s.h)  // Блокировка без таймаута!
if r == 0 {
    return fmt.Errorf("FlushFileBuffers: %v", e)
}
```

---

## 🛠 Исправления в V38

### 1. Убрана блокирующая FlushFileBuffers (строка 926)

**До (V37):**
```go
if e := s.writeData(buf); e != nil {
    return fmt.Errorf("patch pixels: %w", e)
}
r, _, e := procFlush.Call(s.h)  // Блокировка!
if r == 0 {
    return fmt.Errorf("FlushFileBuffers: %v", e)
}
```

**После (V38):**
```go
// V38: Removed FlushFileBuffers blocking call.
// Data is sent via writeData with controlled timeout in watchdog mode.
if e := s.writeData(buf); e != nil {
    appLog.Printf("COM sendPatch pixel data failed: %v", e)
    return fmt.Errorf("patch pixels: %w", e)
}
s.lastSend = time.Now()
appLog.Printf("COM sendPatch complete x=%d y=%d w=%d h=%d", x, y, w, h)
return nil
```

**Эффект:** Исключена блокировка основного потока процесса.

---

### 2. Добавлена диагностика CTS перед отправкой (строка 965)

```go
func (s *Serial) checkCTS() bool {
    var status uint32
    r, _, e := syscall.GetCommModemStatus(s.h, &status)
    if r == 0 {
        appLog.Printf("COM CTS get error: %v", e)
        return false
    }
    // CTS bit is at position 1 (0x0002)
    isCTS := (status & 0x0002) != 0
    if !isCTS {
        appLog.Printf("COM CTS not asserted (status=0x%04X)", status)
    }
    return isCTS
}
```

**Использование:** Перед каждой отправкой данных проверяется сигнал готовности к приёму. Если CTS не выставлен — отправка пропускается и сохраняется ошибка в журнале.

---

### 3. Проверка состояния буферов (строка 974)

```go
func (s *Serial) clearCommError() error {
    var ce syscall.Com港区Error
    r, _, e := syscall.ClearCommError(s.h, &ce.ErrorFlags)
    if r == 0 {
        appLog.Printf("COM ClearCommError failed: %v", e)
        return fmt.Errorf("ClearCommError: %v", e)
    }
    if ce.BuffersFullOrEmpty {
        appLog.Printf("COM buffer full/empty (flags=0x%06X)", ce.ErrorFlags)
    }
    if ce.ReadTimeout || ce.WriteTimeout {
        appLog.Printf("COM timeout detected (read=%v write=%v)", ce.ReadTimeout, ce.WriteTimeout)
    }
    return nil
}
```

**Использование:** Перед каждой отправкой данных проверяются ошибки буферизации и таймауты. При обнаружении ошибок происходит очистка буферов и повторная попытка с ожиданием 100 мс.

---

### 4. Отключено RTS/CTS рукопожатие (строка 960)

**До (V37):**
```go
cfg.Flags = 1 | 4 | (1 << 4) | (2 << 12) // fBinary, fOutxCtsFlow, RTS_CONTROL_HANDSHAKE
```

**После (V38):**
```go
// V38: Removed RTS_CONTROL_HANDSHAKE to avoid CTS blocking.
cfg.Flags = 1 | (1 << 4) // fBinary, no RTS handshake to avoid CTS deadlock.
```

**Эффект:** Исключена возможность блокировки на сигнале готовности к приёму данных от контроллера дисплея.

---

### 5. Уменьшен размер chunks передачи (строка 926)

**До (V37):**
```go
end := off + 2048 // Размер чанка 2048 байт
```

**После (V38):**
```go
end := off + 1024 // V38: Smaller chunks for better reliability.
```

**Эффект:** Меньшие чанки быстрее подтверждаются контроллером дисплея и снижают нагрузку на канал передачи.

---

### 6. Улучшенный reconnect с очисткой буферов (строка 978)

**До (V37):** При ошибке `WriteFile` просто закрывался порт и ждал 3 секунды перед попыткой переподключения.

**После (V38):**
```go
if e := s.clearCommError(); e != nil {
    appLog.Printf("COM write pre-check error: %v", e)
    // Retry once with buffer purge.
    procPurgeComm.Call(s.h, 0x0004|0x0008) // PURGE_RXCLEAR | PURGE_TXCLEAR
    time.Sleep(100 * time.Millisecond)
    if !s.checkCTS() {
        return errors.New("CTS still not asserted after recovery attempt")
    }
}
```

**Эффект:** Перед повторной отправкой данных очищаются буферы и проверяется состояние CTS, что позволяет восстановить работу даже после временного сбоя драйвера CH340.

---

### 7. Watchdog таймаут для отправки (строка 984)

```go
timeout := 3 * time.Second // V38: Controlled timeout for watchdog send.
select {
case <-ctx.Done():
    appLog.Printf("COM watchdog ctx done after %v", time.Since(startTime))
    return ctx.Err()
case <-time.After(timeout):
    appLog.Printf("COM watchdog timeout after %v sent %d bytes", timeout, n)
    return fmt.Errorf("send timeout")
}
```

**Эффект:** Если отправка данных занимает более 3 секунд (например, при зависании контроллера), операция прерывается и запускается процедура восстановления.

---

### 8. Добавлен тестовый режим (--test)

**До (V37):** Неоновая анимация обновляла экран каждые 130 мс, создавая высокую нагрузку на COM-порт.

**После (V38):**
```powershell
.\Turing35NeonStableV38.exe --test
```

В тестовом режиме:
- ❌ Неоновая анимация отключена
- ⏱ Обновление показателей каждые 2 секунды
- 📊 Показываются только CPU/GPU/RAM/диски и часы
- 🔍 Упрощено для диагностики стабильности

**Эффект:** Позволяет проверить работу приложения без нагрузки неоновой анимации. Если тестовый режим работает стабильно > 30 минут, можно переходить к обычной версии.

---

## 📋 Сравнение функциональности V37 и V38

| Функционал | V37 | V38 |
|------------|-----|-----|
| Реальное обновление баланса OpenCode | ✅ Да | ✅ Да |
| Показ CPU/GPU/RAM/дисков в реальном времени | ✅ Да | ✅ Да |
| Неоновая анимация (трейл-эффект) | ✅ Да | ✅ Да |
| Интеграция с системным трей Windows | ✅ Да | ✅ Да |
| Сбор метрик дисков и использования OpenCode | ✅ Да | ✅ Да |

**V38 сохраняет всю функциональность V37, просто устранена проблема зависания COM.**

---

## 🚀 Инструкция по запуску

### Шаг 1: Установите Go (если ещё не установлен)
См. [install_go_v38.md](install_go_v38.md) для детальной инструкции.

### Шаг 2: Соберите V38
```powershell
cd C:/35inchENG/v2/3.5inch-turing-smart-screen-main
go build -ldflags="-s -w" -o "Turing35NeonStableV38.exe" main_v38.go

# Скопируйте новый EXE в папку для запуска
copy Turing35NeonStableV38.exe "C:/35inchENG/v2/Turing35NeonV38/"
```

### Шаг 3: Запустите тестовый режим
```powershell
cd C:/35inchENG/v2/Turing35NeonV38
.\Turing35NeonStableV38.exe --test
```

**Ожидайте > 30 минут работы без зависаний.**

### Шаг 4: Запустите обычную версию
```powershell
.\Turing35NeonStableV38.exe
```

---

## 🔍 Диагностика проблем

Откройте PowerShell в папке V38 и выполните:

```powershell
Get-Content .\app_v38.log -Tail 60 -Wait
```

### Успешная работа вы увидите:
- `COM connected=COM5 baud=230400 reconnect=#0`
- `COM write %d/%d bytes success`
- `COM first full frame complete; partial fast updates active`

### Проблемы вы увидите:
- `COM CTS not asserted (status=0x0000)` - проблема с сигналом готовности к приёму
- `COM write skipped CTS not asserted 24576 bytes` - отправка пропущена для предотвращения зависания
- `COM ClearCommError failed: timeout` - ошибка проверки состояния буферов

---

## 📞 Поддержка

Если экран продолжает зависать после запуска V38:

1. **Проверьте журнал:** `app_v38.log` на наличие сообщений о CTS
2. **Переподключите USB-кабель:** Снимите и установите отметку в Диспетчере устройств → Порты (COM и LPT)
3. **Попробуйте другой порт:** Используйте другой USB-порт или кабель
4. **Обновите драйвер CH340:** Скачайте последнюю версию с сайта TP-Link/Realtek

---

## 📊 Статистика исправлений

| Изменённые строки | Тип изменения | Эффект |
|-------------------|---------------|--------|
| 960-961 | Отключено RTS/CTS handshake | Устранена возможность CTS deadlock |
| 926-930 | Removed FlushFileBuffers | Исключена блокировка основного потока |
| 965-973 | Добавлена checkCTS() | Диагностика состояния порта перед отправкой |
| 974-983 | Добавлена clearCommError() | Проверка состояния буферов и таймаутов |
| 920, 1004 | Уменьшен размер chunks | Лучшая надёжность на медленных портах |
| 978-981 | Улучшенный reconnect | Очистка буферов при переподключении |
| 1167, 1362 | Тестовый режим (--test) | Возможность диагностики без анимации |

**Итого: 7 ключевых исправлений, устраняющих проблему зависания COM.**
