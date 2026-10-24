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
Get-Content -Raw migrations/001_init.sql,migrations/002_create_events.sql,migrations/003_create_delivery_attempts.sql,migrations/004_event_queue.sql | docker compose exec -T db psql -U hookrelay -d hookrelay -v ON_ERROR_STOP=1 --single-transaction -f -
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

`compose.yaml` запускает PostgreSQL 17 с постоянным томом и проверкой готовности. База доступна на `127.0.0.1:5433`; внешний порт задаётся через `POSTGRES_PORT`. SQL-файлы в `migrations` создают таблицы `endpoints`, `events`, `delivery_attempts` и добавляют поля аренды очереди. Для существующей базы с тремя таблицами нужно отдельно применить `004_event_queue.sql` перед запуском обновлённого приложения.

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

При успешном ответе получателя статус станет `delivered`. При сетевой ошибке, таймауте или ответе вне диапазона `2xx` — `failed`.

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

| HTTP-статус | Код | Причина |
| --- | --- | --- |
| `400` | `invalid_json` | Некорректный JSON, неизвестное поле или лишнее JSON-значение |
| `400` | `validation_error` | Нарушено правило проверки входного поля |
| `404` | `not_found` | Endpoint или событие не найдено |
| `409` | `conflict` | Конфликт ID или удаление endpoint со связанными событиями |
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
                             PostgreSQL: delivered/failed
```

- `cmd/hookrelay` открывает пул PostgreSQL, проверяет схему очереди и запускает HTTP-сервер и worker pool. При остановке завершает HTTP-обработчики, прекращает захват новых заданий, ждёт активные отправки и закрывает соединения с БД.
- `internal/domain` содержит модели, входные структуры, валидацию и допустимые переходы статусов.
- `internal/httpapi` обрабатывает запросы и формирует JSON-ответы.
- `internal/repository` определяет CRUD-интерфейс `Store` и интерфейс очереди `DeliveryStore`. Захват задания выполняется в короткой транзакции, завершение проверяет номер и срок аренды. `MemoryStore` используется в быстрых тестах.
- `internal/delivery` опрашивает PostgreSQL каждые 250 мс при пустой очереди или ошибке захвата и выполняет исходящие HTTP-запросы. Во время HTTP-отправки транзакция захвата уже закрыта.
- `examples/receiver` — локальный HTTP-получатель для проверки доставки.
- `internal/postgres` создаёт пул соединений с PostgreSQL и проверяет его доступность.
- `cmd/dbcheck` выполняет проверку подключения к PostgreSQL по `DATABASE_URL`.

`Event.Payload` представлен в Go как `json.RawMessage`, а в PostgreSQL хранится в `JSONB`. База может изменить пробелы и порядок ключей JSON; API сохраняет структуру и значения объекта. SQL-запросы используют параметры `$1`, `$2` и т. д. Ошибка БД возвращается клиенту как `500 internal_error`, отсутствие записи — как `404 not_found`.

## Эксплуатационные ограничения

- После аварии незавершённое событие снова становится доступно по истечении 30-секундной аренды. Воркеры забирают его с учётом очереди и доступности БД. Приложение не сбрасывает активные аренды других экземпляров при старте.
- Восстановление может повторить HTTP-запрос, который получатель уже успел обработать. Получатель должен учитывать `X-HookRelay-Event-ID`, если повторная обработка недопустима; гарантии ровно одной доставки нет.
- Обычная HTTP-ошибка или таймаут отправки приводят к `failed`, без автоматического retry и истории попыток.
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

Интеграционные тесты PostgreSQL проверяют миграции, CRUD, ошибки HTTP API, параллельный захват очереди, аренды и защиту от устаревшего владельца. Отдельный тест принудительно завершает процесс во время HTTP-отправки и восстанавливает задание другим воркером. Тесты запускаются при заданной `TEST_DATABASE_URL` и работают в отдельных схемах; без этой переменной пропускаются. Пример запуска приведён в [документации PostgreSQL](docs/postgres.md).
