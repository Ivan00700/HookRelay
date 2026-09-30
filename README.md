# HookRelay

HookRelay — Go-сервис для доставки webhook-событий. Проект находится в разработке.

## Текущее состояние

- HTTP-сервер с проверкой доступности `GET /health`;
- REST API для endpoints и событий;
- потокобезопасное хранение в памяти процесса;
- проверка входных данных: обязательные поля, HTTP(S) URL и JSON-объект payload.

## Запуск

Требуется Go 1.24 или новее.

```powershell
go run ./cmd/hookrelay
```

По умолчанию сервер слушает порт `8080`. Его можно изменить переменной окружения `HTTP_PORT`:

```powershell
$env:HTTP_PORT = "9090"
go run ./cmd/hookrelay
```

Проверка:

```powershell
curl.exe -i http://localhost:8080/health
```

## API

Создание получателя webhook в PowerShell:

```powershell
$endpointBody = @{
    name = "orders"
    url = "http://localhost:9000/webhook"
    secret = "dev-secret"
} | ConvertTo-Json -Compress
$endpoint = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/endpoints" -Method Post -ContentType "application/json" -Body $endpointBody
$endpoint.id
```

Секрет сохраняется для будущей подписи запросов, но не возвращается через API.
Создание события для полученного endpoint:

```powershell
$eventBody = @{
    endpoint_id = $endpoint.id
    type = "order.created"
    payload = @{ order_id = 42 }
} | ConvertTo-Json -Compress
$event = Invoke-RestMethod -Uri "http://localhost:8080/api/v1/events" -Method Post -ContentType "application/json" -Body $eventBody
$event.status
```

Событие сохраняется со статусом `pending`; отправка получателю пока не
выполняется. Доступны также `GET /api/v1/endpoints`,
`GET /api/v1/endpoints/{id}`, `DELETE /api/v1/endpoints/{id}`,
`GET /api/v1/events` и `GET /api/v1/events/{id}`. Пока хранилище находится
в памяти, поэтому после перезапуска сервера данные теряются.

Проверка кода:

```powershell
go test ./...
go test -race ./...
go vet ./...
```

## Дальнейшее развитие

Конкурентная доставка, PostgreSQL, повторные попытки, HMAC-подпись,
веб-интерфейс и метрики Prometheus.
