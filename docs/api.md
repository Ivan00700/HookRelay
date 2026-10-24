# HTTP API HookRelay

Базовый адрес по умолчанию: `http://localhost:8080`. Для запуска и настройки подключения см. [README](../README.md).

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

## Маршруты

| Метод | Путь | Успешный ответ |
| --- | --- | --- |
| `GET` | `/health` | `200`, `{"status":"ok"}` |
| `GET` | `/ready` | `200`, `{"status":"ready"}`; при ошибке БД `503` |
| `GET` | `/metrics` | `200`, метрики Prometheus |
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
