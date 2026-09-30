# HookRelay

HookRelay — сервис для приёма и последующей доставки webhook-событий на зарегистрированные HTTP-адреса. Отправитель создаёт endpoint получателя, публикует событие и получает его ID. В целевой архитектуре HookRelay доставляет событие асинхронно, сохраняет попытки, повторяет временные ошибки и даёт возможность проследить результат.

Сейчас доступна первая работающая часть: REST API принимает endpoints и события, проверяет запросы и сохраняет данные в потокобезопасном хранилище в памяти. События получают статус `pending`, но **пока не отправляются** получателям. Ни PostgreSQL, ни фоновых воркеров в текущей версии нет.

## Возможности текущей версии

- Создание, получение, просмотр списка и удаление endpoints.
- Приём событий для существующего endpoint; получение события по ID и просмотр списка.
- Проверка обязательных полей, HTTP(S) URL и JSON-объекта в `payload`.
- Один JSON-объект в запросе, запрет неизвестных полей и ограничение тела POST-запроса до 1 MiB.
- ID из 16 случайных байтов (`crypto/rand`), время создания в UTC.
- Единая JSON-форма ошибок; секрет endpoint не включается в ответы API.
- Потокобезопасное хранение в памяти с проверкой связи между endpoint и событием.
- Unit- и HTTP-тесты, включая проверку конкурентного доступа с race detector.

## Быстрый старт

Нужен [Go 1.24](https://go.dev/dl/) или новее. Сторонние зависимости, Docker и база данных для текущей версии не требуются.

```powershell
git clone https://github.com/Ivan00700/HookRelay.git
cd HookRelay
go run ./cmd/hookrelay
```

Сервер слушает `http://localhost:8080`. Для другого порта установите `HTTP_PORT` перед запуском:

```powershell
$env:HTTP_PORT = "9090"
go run ./cmd/hookrelay
```

`HTTP_PORT` — единственная переменная окружения, которую сейчас читает приложение. Если она не задана, используется `8080`. Сервер принимает подключения на всех интерфейсах (`:порт`), поэтому текущую версию без аутентификации не следует выставлять напрямую в публичную сеть.

Проверка запущенного процесса из второго терминала:

```powershell
curl.exe -i http://localhost:8080/health
```

Ожидаемый ответ: `200 OK` и `{"status":"ok"}`. Если задан `HTTP_PORT`, замените `8080` в примерах ниже.

### Создать endpoint

Пример для Windows PowerShell и PowerShell 7:

```powershell
$base = "http://localhost:8080"
$endpointBody = @{
    name   = "orders"
    url    = "http://localhost:9000/webhook"
    secret = "dev-secret"
} | ConvertTo-Json -Compress

$endpoint = Invoke-RestMethod -Uri "$base/api/v1/endpoints" `
    -Method Post -ContentType "application/json" -Body $endpointBody
$endpoint
```

Ответ `201 Created` содержит `id`, `name`, `url` и `created_at`, но не `secret`. Пример:

```json
{
  "id": "8cfc5e5aee2b810a480b3c354456d49c",
  "name": "orders",
  "url": "http://localhost:9000/webhook",
  "created_at": "2026-09-30T12:00:00Z"
}
```

### Создать событие

Используйте ID endpoint из предыдущего ответа:

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

Ответ `201 Created` содержит новый ID события, ID endpoint, тип, вложенный JSON-объект `payload`, статус `pending` и `created_at`:

```json
{
  "id": "c6043b72aa37b96d0c268869789dc52f",
  "endpoint_id": "8cfc5e5aee2b810a480b3c354456d49c",
  "type": "order.created",
  "payload": { "order_id": 42 },
  "status": "pending",
  "created_at": "2026-09-30T12:00:01Z"
}
```

`201` подтверждает приём и сохранение события, **не доставку webhook**.

Для `curl` в Linux/macOS те же запросы выглядят так (подставьте ID из первого ответа во второй):

```bash
curl -i -X POST http://localhost:8080/api/v1/endpoints \
  -H 'Content-Type: application/json' \
  -d '{"name":"orders","url":"http://localhost:9000/webhook","secret":"dev-secret"}'

curl -i -X POST http://localhost:8080/api/v1/events \
  -H 'Content-Type: application/json' \
  -d '{"endpoint_id":"<endpoint-id>","type":"order.created","payload":{"order_id":42}}'
```

### Прочитать данные

```powershell
Invoke-RestMethod -Uri "$base/api/v1/endpoints" -Method Get
Invoke-RestMethod -Uri "$base/api/v1/endpoints/$($endpoint.id)" -Method Get
Invoke-RestMethod -Uri "$base/api/v1/events" -Method Get
Invoke-RestMethod -Uri "$base/api/v1/events/$($event.id)" -Method Get
```

Списки возвращаются как JSON-массивы, в том числе `[]`, если записей нет. Порядок записей в списке — по ID.

### Удалить endpoint

```powershell
curl.exe -i -X DELETE "$base/api/v1/endpoints/$($endpoint.id)"
```

Если с endpoint уже связано событие, вернётся `409 Conflict`: история событий не удаляется вместе с endpoint. Для endpoint без событий успешное удаление возвращает `204 No Content` без тела.

## HTTP API

Все маршруты событий и endpoints начинаются с `/api/v1`. Успешные ответы с телом используют `Content-Type: application/json`. Клиент не может назначить `id`, `created_at` или `status`: эти поля задаёт сервер.

| Метод | Путь | Успех | Возможные ошибки |
| --- | --- | --- | --- |
| `GET` | `/health` | `200` — `{"status":"ok"}` | `405` для другого метода |
| `POST` | `/api/v1/endpoints` | `201` — созданный endpoint | `400`, `409`, `413`, `500` |
| `GET` | `/api/v1/endpoints` | `200` — массив endpoints | — |
| `GET` | `/api/v1/endpoints/{id}` | `200` — endpoint | `404` |
| `DELETE` | `/api/v1/endpoints/{id}` | `204` — пустое тело | `404`, `409` |
| `POST` | `/api/v1/events` | `201` — созданное событие | `400`, `404`, `409`, `413`, `500` |
| `GET` | `/api/v1/events` | `200` — массив событий | — |
| `GET` | `/api/v1/events/{id}` | `200` — событие | `404` |

Тела POST-запросов:

```json
{"name":"orders","url":"https://example.com/webhook","secret":"change-me"}
```

```json
{"endpoint_id":"<endpoint-id>","type":"order.created","payload":{"order_id":42}}
```

`name`, `url`, `secret`, `endpoint_id` и `type` обязательны. URL должен быть абсолютным HTTP(S)-адресом. `payload` должен быть корректным JSON-объектом; строка, массив и `null` не подходят. Пустой JSON, лишнее JSON-значение после объекта и неизвестные поля отклоняются. Тело POST-запроса ограничено 1 MiB.

### Ошибки

Пример ошибки валидации (`400`):

```json
{"error":{"code":"validation_error","field":"name","message":"name is required"}}
```

Для остальных ошибок поле `field` отсутствует:

```json
{"error":{"code":"not_found","message":"endpoint not found"}}
```

| Статус | Код | Значение |
| --- | --- | --- |
| `400` | `invalid_json` | Некорректный, лишний или неожиданный JSON |
| `400` | `validation_error` | Поле нарушает правило валидации; ответ содержит `field` |
| `404` | `not_found` | Endpoint или событие не найдено |
| `409` | `conflict` | Конфликт ID или удаление endpoint со связанными событиями |
| `413` | `invalid_json` | Тело POST-запроса больше 1 MiB |
| `500` | `internal_error` | Неожиданная ошибка сервера; внутренние подробности не выдаются клиенту |

Неподдерживаемый метод для существующего маршрута может получить `405 Method Not Allowed` от `net/http`; это не JSON-ошибка приложения.

## Как это устроено сейчас

```text
HTTP-клиент
    │
    ▼
net/http ServeMux → HTTP-обработчик → декодирование и валидация
                                      │
                                      ▼
                              интерфейс repository.Store
                                      │
                                      ▼
                            MemoryStore (map + RWMutex)
```

- `cmd/hookrelay/main.go` собирает приложение, создаёт **один** `MemoryStore`, регистрирует маршруты и запускает HTTP-сервер.
- `internal/domain` содержит модели `Endpoint`, `Event`, входные структуры, валидацию и правило перехода статусов.
- `internal/httpapi` преобразует HTTP-запросы в доменные операции и результаты обратно в JSON-ответы.
- `internal/repository` задаёт интерфейс хранилища и реализует его в памяти. `RWMutex` защищает обе карты от конкурентного доступа; проверка существования endpoint и сохранение event выполняются под одной блокировкой.

Секрет хранится в `Endpoint`, но скрывается при JSON-кодировании тегом `json:"-"`. `Event.Payload` хранится как `json.RawMessage`; при записи и выдаче он копируется, чтобы внешний код не мог изменить сохранённые байты через общий срез.

В модели определены `pending`, `processing`, `delivered` и `failed`. Новое событие всегда создаётся в `pending`. Функция `CanTransition` уже описывает допустимые переходы (`pending → processing`, затем `processing → delivered/failed`), но обработчик доставки и смена статуса пока не реализованы.

## Ограничения текущей версии

- Данные находятся только в памяти одного процесса и исчезают при его перезапуске.
- События не отправляются по URL endpoint; статусы не меняются после `pending`.
- Нет повторных попыток, истории доставок и ручного повтора.
- Нет аутентификации клиентов, управления доступом, TLS внутри приложения или ограничения частоты запросов. Запускайте сервис в доверенной среде и не публикуйте текущий API напрямую в интернете.
- Секреты пока предназначены для будущей HMAC-подписи; сама подпись ещё не применяется.
- У списков нет пагинации или фильтрации, поэтому in-memory версия рассчитана на небольшие объёмы данных.

## План развития

1. **Доставка событий.** Очередь, worker pool, HTTP POST получателю, таймауты и корректное завершение воркеров.
2. **Постоянное хранение.** PostgreSQL, SQL-миграции и Docker Compose; реализация того же интерфейса репозитория через `pgx`.
3. **Устойчивая очередь.** Выбор готовых событий в транзакции с `FOR UPDATE SKIP LOCKED`, защита от одновременной обработки и восстановление зависших заданий.
4. **Повторные попытки.** История каждой попытки, интервалы повторов, окончательный статус `failed` и ручной повтор.
5. **Подлинность доставки.** HMAC-подпись тела с секретом endpoint и заголовок с ID события.
6. **Интерфейс и наблюдаемость.** Простой dashboard без frontend-фреймворка, структурированные логи, `/ready` и метрики Prometheus.
7. **Итоговая проверка.** Интеграционные тесты PostgreSQL, сценарии с несколькими воркерами и перезапуском, документация архитектуры и воспроизводимый запуск через Docker Compose.

Эти пункты — roadmap, а не описание уже доступных функций или обещание совместимости будущих API с текущими маршрутами.

## Разработка и тестирование

```powershell
go test ./...
go test -race ./...
go vet ./...
```

Тесты `internal/repository` проверяют правила хранения, конфликты, копирование payload и конкурентный доступ. Тесты `internal/httpapi` вызывают маршрутизатор через `httptest`, без открытия сетевого порта: проверяют успешные запросы, ошибки, лимит тела и путь endpoint → event. Тест `internal/domain` проверяет разрешённые и запрещённые переходы статусов.

Для проверки вручную запустите сервер в одном терминале, а запросы из раздела «Быстрый старт» выполните в другом. После перезапуска `GET /api/v1/endpoints` и `GET /api/v1/events` снова вернут `[]` — до подключения PostgreSQL это ожидаемо.
