# Contributing to Turing 3.5 Neon Monitor V38

<div align="center">

## 🤝 Мы рады вашим вкладам!

</div>

---

## 📋 Как внести вклад

### Bug Reports (Отчёты об ошибках)

Если вы нашли баг, пожалуйста:
1. Проверьте существующие [issue](https://github.com/USERNAME/TURING35NEONV38-GITHUB/issues) - возможно он уже обсуждался
2. Создайте новый issue с описанием проблемы
3. Приложите логи из `app_v37.log` (особенно последние 100 строк)
4. Укажите:
   - Windows версия
   - COM порт и скорость передачи
   - Драйвер CH340 (версия)
   - Шаги для воспроизведения

### Feature Requests (Запросы на функции)

Предложите новую функцию с описанием:
- Что вы хотите добавить
- Зачем это нужно
- Пример использования

### Code Contributions (Вклад в код)

#### Требования к коду:
- Соблюдайте существующий стиль кода (Go fmt)
- Добавляйте комментарии к новым функциям
- Тестируйте изменения перед отправкой PR
- Обновляйте документацию при изменении поведения

#### Как отправить PR:
1. Fork этот репозиторий
2. Создайте ветку с названием `feature/НоваяФункция` или `bugfix/НазваниеОшибки`
3. Сделайте изменения
4. Запушьте в вашу ветку
5. Отправьте PR в основную ветку

#### Процесс проверки:
- [ ] Код проходит `go fmt` и `go vet`
- [ ] Все тесты проходят (`go test ./...`)
- [ ] Обновлена документация при изменении API
- [ ] Добавлены комментарии к новым функциям
- [ ] PR описывает изменения и их влияние на существующий функционал

---

## 📊 Кодовая база V38

### Структура проекта:
```
Turing35NeonV38-GitHub/
├── main_v38.go                    # Основной код приложения
├── README.md                      # Основная документация
├── RELEASE_NOTES.md               # Список изменений
├── STATUS_REPORT.md               # Отчёт о статусе исправлений
├── FINAL_INSTRUCTION.md           # Итоговая инструкция
├── QUICKSTART.md                  # Быстрый старт
├── CONTRIBUTING.md                # Этот файл
├── LICENSE                        # Лицензия MIT
├── CHANGELOG.md                   # История изменений
└── docs/
    ├── INSTALLATION.md            # Инструкция установки
    └── TROUBLESHOOTING.md         # Устранение проблем

```

### Основные компоненты:

1. **Serial Communication** (`main_v38.go`)
   - `Serial` struct для управления COM портом
   - `checkCTS()` - проверка сигнала готовности к приёму
   - `clearCommError()` - проверка состояния буферов
   - `connect()` - подключение с очисткой буферов

2. **Display Rendering**
   - `render()` - генерация изображения для дисплея
   - `sendPatch()` - отправка части изображения на дисплей
   - Неон анимация и трейл эффект

3. **Metrics Collection**
   - CPU, GPU, RAM мониторинг
   - Дисковое пространство и использование OpenCode AI
   - Баланс аккаунта

---

## 🧪 Тестирование изменений

### Локальное тестирование:
```powershell
# Сборка с изменениями
cd C:\35inchENG\v2\3.5inch-turing-smart-screen-main
go build -ldflags="-s -w" -o Turing35NeonStableV38.exe main_v38.go

# Тестирование в тестовом режиме
Turing35NeonStableV38.exe --test
```

### Проверка стабильности:
- Запуските в тестовом режиме `--test` минимум на 30 минут
- Отслеживайте логи через трей → Открыть лог
- Ищите сообщения о CTS или ошибках WriteFile

### Единицы измерения метрик:
```powershell
# CPU/GPU/RAM показатели
Get-CimInstance Win32_Processor | Select-Object LoadPercentage
Get-CimInstance Win32_PerfRawData -Name "*Memory*" | Select-Object CommitCharge
Get-CimInstance Win32_VideoController | Select-Object DedDedicatedVideoMemory

# Дисковое пространство
Get-CimInstance Win32_LogicalDisk | Where-Object {$_.DeviceID -like 'C:\'} | Select-Object DeviceID,FreeSpace,Size
```

---

## 📝 Руководство по ревью PR

### Для авторов PR:
1. Кратко опишите изменения
2. Объясните почему это нужно
3. Укажите как тестировать изменения
4. Приложите скриншоты перед/после при необходимости

### Для ревьюеров:
1. Проверьте что код соответствует стилю проекта
2. Убедитесь что тесты проходят
3. Проверьте производительность изменений
4. Обновите документацию при необходимости
5. Оставьте конструктивную обратную связь

---

## 🎯 Цели проекта на будущее

### V39 Planned Features:
- [ ] USB hot-swap detection (без отключения/включения)
- [ ] Multi-display support (поддержка нескольких дисплеев)
- [ ] Configuration file (замена hard-coded настроек)
- [ ] Better error recovery mechanisms
- [ ] Command-line arguments for configuration

### Performance Improvements:
- [ ] Async COM operations для уменьшения задержек
- [ ] Memory profiling и оптимизация
- [ ] Reduced battery consumption на ноутбуках

---

## 🌐 Сообщество и ресурсы

### Ресурсы:
- [Go Documentation](https://golang.org/doc/) - Документация по Go
- [CH340 Driver GitHub](https://github.com/F fu/CH340_Driver) - Драйверы CH340
- [Windows COM API](https://learn.microsoft.com/en-us/windows/win32/api/commdrv/) - Windows API для COM портов

### Сообщество:
- [GitHub Issues](https://github.com/USERNAME/TURING35NEONV38-GITHUB/issues) - Отчёты об ошибках и фичи
- [Discussion Board](https://github.com/USERNAME/TURING35NEONV38-GITHUB/discussions) - Обсуждения

---

## 📜 Кодекс поведения

Мы стремимся создать дружелюбное, безопасное и открытое пространство для всех участников сообщества.

### Правила:
- Будьте уважительны к другим участникам
- Конструктивно критикуйте идеи, а не людей  
- Давайте друг другу пользу и поддержку
- Соблюдайте этикет GitHub

---

## 📝 Лицензия

Настоящий документ распространяется в соответствии с [MIT License](./LICENSE).

Код и документация доступны для изучения и модификации.

---

<div align="center">

Спасибо за ваш интерес к проекту! Если у вас есть вопросы - не стесняйтесь задавать их через issue или discussion board.

</div>

<div align="center">
  <a href="./README.md"><img src="https://img.shields.io/badge/Back%20to%20README-000000?style=for-the-badge" height="20"></a>
</div>
