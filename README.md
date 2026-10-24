# HookRelay

HookRelay — сервис асинхронной доставки webhook-событий на зарегистрированные HTTP-адреса. Клиент создаёт endpoint получателя, публикует событие и получает его идентификатор. Сервис ставит событие в очередь, отправляет JSON-тело получателю и сохраняет результат доставки.

## Возможности

- REST API для создания, получения, просмотра списка и удаления endpoints.
- Приём событий, связанных с существующим endpoint, и чтение их статусов через API.
- Проверка входных данных: обязательные поля, абсолютный HTTP(S) URL, JSON-объект в `payload`, запрет неизвестных полей и ограничение тела запроса до 1 MiB.
- Хранение endpoints, событий и их статусов в PostgreSQL с сохранением данных после перезапуска.
- Очередь в PostgreSQL и два параллельных worker для исходящих HTTP POST-запросов.
- Атомарный захват задания через `FOR UPDATE SKIP LOCKED`, аренда на 30 секунд и восстановление незавершённой обработки.
- Таймаут доставки 3 секунды. Ответы `2xx` считаются успешными; редиректы не выполняются.
- Автоматические повторы при временных ошибках: через 5 секунд, 30 секунд и 2 минуты, максимум четыре попытки за цикл.
- История попыток с временем, HTTP-кодом и ошибкой; ручная повторная доставка неуспешного события.
- Статусы `pending`, `processing`, `delivered` и `failed` с проверкой допустимых переходов.
- Единый JSON-формат ошибок и штатное завершение HTTP-сервера и worker pool.

Идентификаторы состоят из 16 случайных байтов, представленных 32 шестнадцатеричными символами. Время создания записывается в UTC. Секрет endpoint сохраняется в базе данных, но не включается в ответы API.

## Запуск

Требуются Go 1.24 или новее и PostgreSQL. Для локального запуска PostgreSQL 17 используется Docker Compose. Go автоматически скачивает зависимости из `go.mod`.

```powershell
git clone https://github.com/Ivan00700/HookRelay.git
cd HookRelay
if (-not (Test-Path .env)) { Copy-Item .env.example .env }
```

Задайте свой `POSTGRES_PASSWORD` в `.env`, затем запустите базу:

```powershell
docker compose up -d --wait db
```

Для пустой базы примените SQL-файлы. Для базы с уже созданными таблицами этот шаг повторять не нужно:

```powershell
Get-Content -Raw migrations/001_init.sql,migrations/002_create_events.sql,migrations/003_create_delivery_attempts.sql,migrations/004_event_queue.sql,migrations/005_delivery_retries.sql | docker compose exec -T db psql -U hookrelay -d hookrelay -v ON_ERROR_STOP=1 --single-transaction -f -
```

В том же PowerShell задайте подключение, подставив пароль из `.env`, и запустите приложение:

```powershell
$databasePassword = [uri]::EscapeDataString('пароль из POSTGRES_PASSWORD')
$env:DATABASE_URL = "postgres://hookrelay:${databasePassword}@127.0.0.1:5433/hookrelay?sslmode=disable"
go run ./cmd/hookrelay
```

При другом `POSTGRES_PORT` укажите тот же порт в `DATABASE_URL`. Go читает `DATABASE_URL` из окружения процесса; `.env` автоматически загружает только Compose. При отсутствии настройки или ошибке подключения сервер завершается с ошибкой до начала приёма запросов.

По умолчанию сервер слушает порт `8080` на всех сетевых интерфейсах. Порт можно изменить переменной окружения `HTTP_PORT`:

```powershell
$env:HTTP_PORT = "9090"
go run ./cmd/hookrelay
```

В другом терминале проверьте доступность:

```powershell
curl.exe -i http://localhost:8080/health
```

Ответ: `200 OK` и `{"status":"ok"}`. Если задан другой `HTTP_PORT`, используйте его и в последующих примерах.

Для локальной проверки доставки запустите HTTP-получателя:

```powershell
go run ./examples/receiver
```

Он принимает `POST http://127.0.0.1:9000/webhook`, выводит идентификатор события и отвечает `204 No Content`.

## PostgreSQL

`compose.yaml` запускает PostgreSQL 17 с постоянным томом и проверкой готовности. База доступна на `127.0.0.1:5433`; внешний порт задаётся через `POSTGRES_PORT`. SQL-файлы в `migrations` создают таблицы `endpoints`, `events`, `delivery_attempts`, поля аренды и расписания повторов. Если файлы `001`–`004` уже применены, остановите приложение и примените только `005_delivery_retries.sql`. Порядок обновления описан в документации PostgreSQL.

Команда `go run ./cmd/dbcheck` проверяет соединение через `pgx`, выводит имя базы, пользователя и версию сервера, затем закрывает соединения. Она и HTTP-сервис используют переменную `DATABASE_URL`. Endpoints, события и результаты смены статуса сохраняются через `PostgresStore`.

Настройка, применение SQL-файлов, подключение DBeaver и запуск проверки описаны в [документации PostgreSQL](docs/postgres.md).

## Пример работы с API

Примеры ниже рассчитаны на Windows PowerShell или PowerShell 7. Сначала создайте endpoint:

```powershell
$base = "http://localhost:8080"
$endpointBody = @{
    name   = "orders"
    url    = "http://127.0.0.1:9000/webhook"
    secret = "dev-secret"
} | ConvertTo-Json -Compress

$endpoint = Invoke-RestMethod -Uri "$base/api/v1/endpoints" `
    -Method Post -ContentType "application/json" -Body $endpointBody
$endpoint
```

Ответ `201 Created` содержит `id`, `name`, `url` и `created_at`, но не содержит `secret`. Затем опубликуйте событие для созданного endpoint:

```powershell
$eventBody = @{
    endpoint_id = $endpoint.id
    type        = "order.created"
    payload     = @{ order_id = 42 }
} | ConvertTo-Json -Compress

$event = Invoke-RestMethod -Uri "$base/api/v1/events" `
    -Method Post -ContentType "application/json" -Body $eventBody
$event
```

`201 Created` подтверждает сохранение события в PostgreSQL: запись `pending` уже является заданием очереди, даже если воркеры остановлены. В ответе на создание статус равен `pending`; воркер может успеть изменить сохранённый статус до ответа API. Результат доставки нужно запросить отдельно:

```powershell
Invoke-RestMethod -Uri "$base/api/v1/events/$($event.id)" -Method Get
```

При успешном ответе получателя статус станет `delivered`. Сетевые ошибки, таймауты и ответы `408`, `429`, `5xx` допускают повтор; до его срока событие остаётся `pending`. Другие ответы вне `2xx` завершают доставку со статусом `failed` сразу. После четвёртой неудачной попытки временная ошибка также приводит к `failed`.

Посмотреть историю и повторно отправить событие со статусом `failed`:

```powershell
Invoke-RestMethod -Uri "$base/api/v1/events/$($event.id)/attempts" -Method Get
Invoke-RestMethod -Uri "$base/api/v1/events/$($event.id)/retry" -Method Post
```

Ручной повтор возвращает `202 Accepted` и начинает новый цикл из максимум четырёх попыток. ID события и история сохраняются. Для `pending`, `processing` или `delivered` возвращается `409 Conflict`, для неизвестного ID — `404 Not Found`.

Остальные операции чтения:

```powershell
Invoke-RestMethod -Uri "$base/api/v1/endpoints" -Method Get
Invoke-RestMethod -Uri "$base/api/v1/endpoints/$($endpoint.id)" -Method Get
Invoke-RestMethod -Uri "$base/api/v1/events" -Method Get
```

Удаление endpoint без связанных событий возвращает `204 No Content`:

```powershell
curl.exe -i -X DELETE "$base/api/v1/endpoints/$($endpoint.id)"
```

Если с endpoint уже связано событие, сервис возвращает `409 Conflict`; событие при этом не удаляется.

Для клиентов без PowerShell эквивалентные запросы можно отправить через `curl`:

```bash
curl -i -X POST http://localhost:8080/api/v1/endpoints \
  -H 'Content-Type: application/json' \
  -d '{"name":"orders","url":"http://127.0.0.1:9000/webhook","secret":"dev-secret"}'

curl -i -X POST http://localhost:8080/api/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"endpoint_id":"<endpoint-id>","type":"order.created","payload":{"order_id":42}}'
```

Подставьте в `<endpoint-id>` идентификатор из первого ответа.

## HTTP API

| Метод | Путь | Успешный ответ |
| --- | --- | --- |
| `GET` | `/health` | `200`, `{"status":"ok"}` |
| `POST` | `/api/v1/endpoints` | `201`, созданный endpoint |
| `GET` | `/api/v1/endpoints` | `200`, массив endpoints |
| `GET` | `/api/v1/endpoints/{id}` | `200`, endpoint |
| `DELETE` | `/api/v1/endpoints/{id}` | `204`, пустое тело |
| `POST` | `/api/v1/events` | `201`, принятое событие |
| `GET` | `/api/v1/events` | `200`, массив событий |
| `GET` | `/api/v1/events/{id}` | `200`, событие |
| `GET` | `/api/v1/events/{id}/attempts` | `200`, история попыток по номеру, включая незавершённую |
| `POST` | `/api/v1/events/{id}/retry` | `202`, событие в статусе `pending`; тело запроса не требуется |

Тело создания endpoint:

```json
{"name":"orders","url":"https://example.com/webhook","secret":"change-me"}
```

Тело создания события:

```json
{"endpoint_id":"<endpoint-id>","type":"order.created","payload":{"order_id":42}}
```

`name`, `url`, `secret`, `endpoint_id` и `type` обязательны. `payload` должен быть JSON-объектом: строка, массив и `null` отклоняются. Клиент не назначает `id`, `created_at` или `status`. Списки возвращаются массивами, включая `[]` при отсутствии записей; порядок — по ID.

Ошибки возвращаются в едином формате:

```json
{"error":{"code":"validation_error","field":"name","message":"name is required"}}
```

Поле `field` присутствует только у ошибок валидации.

Запись истории содержит `id`, `event_id`, `attempt_number`, `started_at`, `finished_at`, `http_status` и при ошибке `error_message`. До завершения попытки `finished_at` равен `null`; если HTTP-ответ не получен, `http_status` равен `null`. Номера попыток продолжаются после ручного повтора. Для события без попыток возвращается `[]`.

| HTTP-статус | Код | Причина |
| --- | --- | --- |
| `400` | `invalid_json` | Некорректный JSON, неизвестное поле или лишнее JSON-значение |
| `400` | `validation_error` | Нарушено правило проверки входного поля |
| `404` | `not_found` | Endpoint или событие не найдено |
| `409` | `conflict` | Конфликт ID, удаление endpoint со связанными событиями или ручной повтор события не в статусе `failed` |
| `413` | `invalid_json` | Тело POST-запроса превышает 1 MiB |
| `500` | `internal_error` | Внутренняя ошибка сервера |

Для неподдерживаемого метода `net/http` может вернуть `405 Method Not Allowed` без JSON-тела приложения.

## Устройство сервиса

```text
HTTP-клиент
    │
    ▼
ServeMux → HTTP-обработчик → валидация → PostgreSQL: pending
                                            ↑
                                  worker: захват и аренда
                                            ↓
                                  HTTP POST → получатель
                                            ↓
                       PostgreSQL: история + delivered/failed
                              или pending с датой повтора
```

- `cmd/hookrelay` открывает пул PostgreSQL, проверяет схему очереди и запускает HTTP-сервер и worker pool. При остановке завершает HTTP-обработчики, прекращает захват новых заданий, ждёт активные отправки и закрывает соединения с БД.
- `internal/domain` содержит модели, входные структуры, валидацию, переходы статусов и интервалы повторов.
- `internal/httpapi` обрабатывает запросы и формирует JSON-ответы.
- `internal/repository` определяет CRUD-интерфейс `Store` и интерфейс очереди `DeliveryStore`. Захват задания и начало записи истории выполняются одной короткой транзакцией. Другая транзакция сохраняет результат попытки и статус события с проверкой версии и срока аренды. `MemoryStore` используется в быстрых тестах.
- `internal/delivery` опрашивает PostgreSQL каждые 250 мс при пустой очереди или ошибке захвата и выполняет исходящие HTTP-запросы. Во время HTTP-отправки транзакция захвата уже закрыта.
- `examples/receiver` — локальный HTTP-получатель для проверки доставки.
- `internal/postgres` создаёт пул соединений с PostgreSQL и проверяет его доступность.
- `cmd/dbcheck` выполняет проверку подключения к PostgreSQL по `DATABASE_URL`.

`Event.Payload` представлен в Go как `json.RawMessage`, а в PostgreSQL хранится в `JSONB`. База может изменить пробелы и порядок ключей JSON; API сохраняет структуру и значения объекта. SQL-запросы используют параметры `$1`, `$2` и т. д. Ошибка БД возвращается клиенту как `500 internal_error`, отсутствие записи — как `404 not_found`.

## Эксплуатационные ограничения

- После аварии незавершённое событие снова становится доступно по истечении 30-секундной аренды. Воркеры забирают его с учётом очереди и доступности БД. Приложение не сбрасывает активные аренды других экземпляров при старте.
- Восстановление может повторить HTTP-запрос, который получатель уже успел обработать. Получатель должен учитывать `X-HookRelay-Event-ID`, если повторная обработка недопустима; гарантии ровно одной доставки нет.
- Интервалы повторов отсчитываются от сохранения результата предыдущей попытки; фактический запуск зависит от свободных воркеров. Расписание хранится в `next_attempt_at` и сохраняется при перезапуске. Заголовок `Retry-After` не изменяет фиксированные интервалы.
- Захват задания расходует попытку, даже если процесс упал до отправки HTTP. После истечения аренды незавершённая попытка помечается прерванной; после четырёх захватов цикл завершается статусом `failed`. Восстановление по аренде не использует обычные интервалы повторов.
- Секрет endpoint не возвращается через API, но исходящие запросы не подписываются им. Сам факт наличия секрета не подтверждает подлинность доставки.
- В API нет аутентификации, TLS и ограничения частоты запросов. Сервер слушает все интерфейсы; запускайте его только в доверенной среде, не открывая напрямую в публичную сеть.
- Списки не поддерживают пагинацию и фильтрацию и возвращаются целиком.

## Проверка

```powershell
go test ./...
go test -race ./...
go vet ./...
```

Тесты охватывают валидацию и переходы статусов, операции хранилища, конкурентный доступ, отправку HTTP-запросов, таймауты, остановку worker pool и HTTP API. Для ручной проверки запустите сервис и локального получателя в отдельных терминалах, затем выполните запросы из раздела «Пример работы с API».

Интеграционные тесты PostgreSQL проверяют миграции, CRUD, ошибки HTTP API, параллельный захват очереди, аренды и защиту от устаревшего владельца, расписание повторов, историю, лимит попыток и ручной повтор. Отдельный тест принудительно завершает процесс во время HTTP-отправки и восстанавливает задание другим воркером. Тесты запускаются при заданной `TEST_DATABASE_URL` и работают в отдельных схемах; без этой переменной пропускаются. Пример запуска приведён в [документации PostgreSQL](docs/postgres.md).
