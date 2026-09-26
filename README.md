# HookRelay

HookRelay — Go-сервис для доставки webhook-событий. Проект находится в разработке.

## Текущее состояние

- HTTP-сервер с проверкой доступности `GET /health`;
- модели endpoint и события;
- проверка входных данных: обязательные поля, HTTP(S) URL, JSON-объект payload и статус доставки.

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

## Дальнейшее развитие

REST API для endpoints и событий, конкурентная доставка, PostgreSQL, повторные попытки, HMAC-подпись, веб-интерфейс и метрики Prometheus.
