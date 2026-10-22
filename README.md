# HookRelay

HookRelay — сервис асинхронной доставки webhook-событий на зарегистрированные HTTP-адреса. Клиент создаёт endpoint получателя, публикует событие и получает его идентификатор. Сервис ставит событие в очередь, отправляет JSON-тело получателю и сохраняет результат доставки.

## Возможности

- REST API для создания, получения, просмотра списка и удаления endpoints.
- Приём событий, связанных с существующим endpoint, и чтение их статусов через API.
- Проверка входных данных: обязательные поля, абсолютный HTTP(S) URL, JSON-объект в `payload`, запрет неизвестных полей и ограничение тела запроса до 1 MiB.
- Потокобезопасное in-memory хранилище endpoints и событий.
- Очередь на 64 задания и два параллельных worker для исходящих HTTP POST-запросов.
- Таймаут доставки 3 секунды. Ответы `2xx` считаются успешными; редиректы не выполняются.
- Статусы `pending`, `processing`, `delivered` и `failed` с проверкой допустимых переходов.
- Единый JSON-формат ошибок и штатное завершение HTTP-сервера и worker pool.

Идентификаторы состоят из 16 случайных байтов, представленных 32 шестнадцатеричными символами. Время создания записывается в UTC. Секрет endpoint принимается и хранится в памяти приложения, но не включается в ответы API.

## Запуск

Требуется Go 1.24 или новее. Go автоматически скачивает зависимости из `go.mod`. HTTP-сервис использует хранилище в памяти и запускается без отдельной базы данных.

```powershell
git clone https://github.com/Ivan00700/HookRelay.git
cd HookRelay
go run ./cmd/hookrelay
```

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

`compose.yaml` запускает PostgreSQL 17 с постоянным томом и проверкой готовности. База доступна на `127.0.0.1:5433`; внешний порт задаётся через `POSTGRES_PORT`. SQL-файлы в `migrations` создают таблицы `endpoints`, `events` и `delivery_attempts`.

Команда `go run ./cmd/dbcheck` проверяет соединение через `pgx`, выводит имя базы, пользователя и версию сервера, затем закрывает соединения. Для неё требуется переменная `DATABASE_URL`. HTTP API хранит данные в `MemoryStore` и не записывает их в PostgreSQL.

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

`201 Created` подтверждает приём события и постановку в очередь, а не завершение доставки. В ответе на создание статус равен `pending`. Worker может изменить сохранённый статус сразу после постановки задания в очередь, поэтому результат нужно запросить отдельно:

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
| `503` | `delivery_unavailable` | Очередь доставки закрыта |

Для неподдерживаемого метода `net/http` может вернуть `405 Method Not Allowed` без JSON-тела приложения.

## Устройство сервиса

```text
HTTP-клиент
    │
    ▼
ServeMux → HTTP-обработчик → валидация → MemoryStore: pending
                                       │
                                       └→ очередь ID → worker → HTTP POST → получатель
                                                      │
                                                      └→ MemoryStore: processing → delivered/failed
```

- `cmd/hookrelay` создаёт HTTP-сервер, общее хранилище, HTTP-клиент и worker pool; также управляет остановкой приложения.
- `internal/domain` содержит модели, входные структуры, валидацию и допустимые переходы статусов.
- `internal/httpapi` обрабатывает запросы и формирует JSON-ответы.
- `internal/repository` определяет интерфейс хранилища и его реализацию на `map` с `sync.RWMutex`.
- `internal/delivery` управляет очередью и выполняет исходящие HTTP-запросы.
- `examples/receiver` — локальный HTTP-получатель для проверки доставки.
- `internal/postgres` создаёт пул соединений с PostgreSQL и проверяет его доступность.
- `cmd/dbcheck` выполняет проверку подключения к PostgreSQL по `DATABASE_URL`.

`Event.Payload` хранится как `json.RawMessage`. Хранилище копирует его байты при сохранении и выдаче, чтобы внешние изменения среза не меняли уже сохранённое событие.

## Эксплуатационные ограничения

- Данные и задания находятся в памяти одного процесса. После завершения или сбоя процесса они не сохраняются.
- Для каждого события выполняется одна попытка доставки; повторных отправок и истории попыток нет.
- При заполнении очереди запрос создания события ждёт свободного места.
- Секрет endpoint не возвращается через API, но исходящие запросы не подписываются им. Сам факт наличия секрета не подтверждает подлинность доставки.
- В API нет аутентификации, TLS и ограничения частоты запросов. Сервер слушает все интерфейсы; запускайте его только в доверенной среде, не открывая напрямую в публичную сеть.
- Списки не поддерживают пагинацию и фильтрацию. In-memory хранилище рассчитано на небольшие объёмы данных.

## Проверка

```powershell
go test ./...
go test -race ./...
go vet ./...
```

Тесты охватывают валидацию и переходы статусов, операции хранилища, конкурентный доступ, отправку HTTP-запросов, таймауты, остановку worker pool и HTTP API. Для ручной проверки запустите сервис и локального получателя в отдельных терминалах, затем выполните запросы из раздела «Пример работы с API».

Интеграционный тест подключения к PostgreSQL запускается при заданной `TEST_DATABASE_URL`; без этой переменной он пропускается. Пример запуска приведён в [документации PostgreSQL](docs/postgres.md).
