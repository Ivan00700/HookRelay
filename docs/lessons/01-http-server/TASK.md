# Этап 1 — первый HTTP-сервер

Статус: завершён.

## Цель

Понять минимальный путь HTTP-запроса внутри Go-приложения:

```text
клиент → http.Server → ServeMux → healthHandler → ResponseWriter → клиент
```

## Реализовано

- запуск сервера через явно созданный `http.Server`;
- собственный `http.ServeMux`;
- маршрут `GET /health`;
- JSON-ответ `{"status":"ok"}`;
- ответ 405 для неподдерживаемых методов;
- выбор порта через `HTTP_PORT` с fallback на 8080.

## Проверки

```powershell
gofmt -w ./cmd/hookrelay/main.go
go vet ./...
go test ./...
curl.exe -i http://localhost:8080/health
curl.exe -i -X POST http://localhost:8080/health
```

Итоговый коммит: `7c10f36 feat: add initial health HTTP server`.
