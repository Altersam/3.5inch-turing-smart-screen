# OpenCode — диагностика V37 (нативный API)

`auth` — сессия старых страниц OpenCode; `__Host-console_session` — отдельная cookie текущей Console. API баланса `/console/api/billing/status` требует console-cookie и заголовок `x-org-id`. Используется `balanceMicroCents`, **не** `availableMicroCents`.

Пример без реальных значений:

```
GET https://opencode.ai/console/api/billing/status
Cookie: __Host-console_session=<private>
x-org-id: wrk_EXAMPLE
Accept: application/json

{"balanceMicroCents":"2786781005"}
```

Значение отображается как `$27.87`. Реальный баланс может меняться после каждого запроса модели. Данные usage Д/Н/М поступают независимо от баланса.

Если в логе `billing/status HTTP=401`, обновите `opencode_console_cookie.txt` локально. Не присылайте cookie в чат и не прикрепляйте экспорт HAR/Copy as cURL. Если `workspace not configured`, создайте `opencode_url.txt` с адресом своего workspace.

В V35 wrapper уже возвращал корректный баланс, но renderer из V29 не получал его через локальный bridge. В V36 значение записывается непосредственно в общую структуру метрик и попадает в `render()`, поэтому bridge полностью исключён.
