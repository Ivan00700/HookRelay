# Логи и метрики

HookRelay пишет структурированные JSON-логи в stderr и отдаёт метрики Prometheus на GET `/metrics`. GET `/health` проверяет работу HTTP-сервера, GET `/ready` — доступность PostgreSQL и наличие полей очереди.

## Запуск и проверки

Запустите приложение с DATABASE_URL согласно README:

```powershell
go run ./cmd/hookrelay
curl.exe -i http://localhost:8080/health
curl.exe -i -H "X-Request-ID: demo-1" http://localhost:8080/ready
curl.exe http://localhost:8080/metrics
```

| Путь | Результат |
| --- | --- |
| `/health` | `200`, `{"status":"ok"}`, независимо от состояния БД |
| `/ready` | `200`, `{"status":"ready"}`, если соединение и схема очереди доступны |
| `/ready` при ошибке | `503`, `{"status":"not_ready"}` |
| `/metrics` | Текстовый формат метрик Prometheus, поддерживается OpenMetrics |

Проверка ready и чтение состояния для метрик ограничены двумя секундами. Readiness проверяет соединение и поля очереди; она не проверяет внешних получателей webhook. При ошибке сбора из БД `/metrics` остаётся доступен, `hookrelay_database_up` становится `0`, а значения состояния событий и очереди не публикуются. Неизвестный размер очереди не подменяется нулём.

Для `/metrics` разрешён один одновременный сбор. Дополнительный одновременный запрос может получить `503`.

## Request ID и поиск события

Middleware добавляет `X-Request-ID` в HTTP-ответ и контекст обработчика. Можно передать свой ID из 1–64 латинских букв, цифр, дефисов и подчёркиваний. Пустой, неверный или повторённый заголовок заменяется новым случайным ID из 32 hex-символов.

Пример строки приёма события:

```json
{"time":"2026-10-24T17:13:24+03:00","level":"INFO","msg":"event accepted","request_id":"demo-1","event_id":"<event-id>","endpoint_id":"<endpoint-id>"}
```

В завершении HTTP-запроса есть method, route, status, duration_seconds и response_bytes. У ошибок HTTP уровень WARN для 4xx и ERROR для 5xx. В логах используется шаблон маршрута без query string и реального ID в URL.

Воркеры пишут event_id, attempt_number, cycle_attempt, HTTP-код, итоговый статус и задержку повтора. По request_id можно найти приём, взять event_id и найти дальнейшие попытки. Request ID не сохраняется в БД и не переносится через очередь; связь фоновой работы строится по event_id.

Секрет endpoint, payload, Authorization и URL получателя не записываются. Для ошибок используется error_kind вместо сырого текста, который может содержать пароль или query-параметры. Например, timeout, network, invalid_request, lease_lost, postgres_23505. Полное сообщение попытки по-прежнему хранится в БД и доступно через существующий API истории.

Request ID служит для поиска логов и не является средством аутентификации. Логи команды dbcheck также идут в stderr как JSON; её диагностический результат подключения остаётся обычной строкой stdout.

## Метрики

| Имя | Тип | Значение |
| --- | --- | --- |
| `hookrelay_events_accepted_total` | Counter | Новые события, сохранённые через HTTP API этим процессом |
| `hookrelay_delivery_attempts_total{result}` | Counter | Сохранённые воркерами результаты попыток, result = success/error |
| `hookrelay_delivery_results_total{status}` | Counter | Завершения циклов доставки, status = delivered/failed |
| `hookrelay_http_requests_total{method,route,status}` | Counter | Завершённые HTTP-запросы |
| `hookrelay_http_request_duration_seconds{method,route}` | Histogram | Длительность HTTP-запросов в секундах |
| `hookrelay_events{status}` | Gauge | Текущее количество записей PostgreSQL в каждом из четырёх статусов |
| `hookrelay_queue_events` | Gauge | Сумма pending и processing в PostgreSQL |
| `hookrelay_database_up` | Gauge | Успех последнего чтения состояния для текущего сбора |

Pending включает отложенные повторы, processing — все записи в обработке, включая ожидающие восстановления аренды. Состояние читается из БД при каждом scrape. В метках нет ID события, endpoint, request ID, URL, произвольного типа события или текста ошибки. Неизвестные пути имеют route = unmatched, нестандартные методы — method = OTHER.

Counter относится к одному процессу и обнуляется после его перезапуска. Ручной retry не увеличивает events_accepted_total, но новый цикл снова может завершиться delivered или failed. Delivery results считает завершённые циклы, а не уникальные ID.

Результаты попытки и статус события сначала сохраняются в БД, затем увеличиваются счётчики. Потерянная аренда не считается сохранённой доставкой. Сбой процесса между COMMIT и увеличением счётчика может пропустить наблюдение. Восстановление прерванных попыток также видно в БД, но не создаёт фиктивного HTTP-результата в Counter. Текущее состояние PostgreSQL доступно отдельно через Gauge и API.

HTTP-гистограмма измеряет весь обработчик. Запрос `/metrics` попадает в HTTP-метрики после формирования своего ответа и виден при следующем сборе.

## Prometheus через Docker Compose

В Compose есть необязательный профиль monitoring:

```powershell
docker compose --profile monitoring up -d --wait prometheus
```

Откройте `http://localhost:9090/`. Конфигурация находится в `deploy/prometheus.yaml`; Prometheus запрашивает контейнер приложения на `app:8080` каждые пять секунд. Изменение внешнего HTTP_PORT не меняет внутренний порт. Сначала запустите `docker compose up -d --build --wait`. Если Go-приложение работает непосредственно в Windows, замените target на `host.docker.internal:8080` (или выбранный HTTP_PORT) и перезапустите Prometheus.

Порт интерфейса Prometheus можно изменить переменной PROMETHEUS_PORT. По умолчанию он опубликован только на `127.0.0.1:9090`. История хранится в именованном томе prometheus_data с retention 24 часа.

На странице Targets у job hookrelay должно быть состояние UP. В выражениях можно проверить:

```promql
up{job="hookrelay"}
sum(rate(hookrelay_events_accepted_total[5m]))
sum by (result) (rate(hookrelay_delivery_attempts_total[5m]))
max(hookrelay_queue_events{job="hookrelay"})
histogram_quantile(0.95, sum by (le, route) (rate(hookrelay_http_request_duration_seconds_bucket[5m])))
```

При нескольких экземплярах приложения Gauge событий и очереди показывают одну общую БД. Их нельзя суммировать между экземплярами; для общего размера используйте max. Счётчики процессов можно суммировать через rate.

Остановить только Prometheus:

```powershell
docker compose stop prometheus
```

Приложение и `/metrics` работают без контейнера Prometheus. В сервисе нет аутентификации; служебные маршруты предназначены для той же доверенной среды, что и API.

## Проверки реализации

Проверены генерация и перенос request ID, допустимые метки, статусы и длительности HTTP, конкурентное увеличение счётчиков, отказ БД, сохранение состояния после перезапуска и потеря аренды. Полный набор с PostgreSQL и race detector проходит.

Отдельно проверен собранный бинарник с настоящим Prometheus: target UP, два принятых события, один delivered, один failed и пустая очередь. JSON-логи содержат связь request_id/event_id и не содержат проверочные секреты, payload и query-параметры. Проверка выполнялась в отдельной схеме и отдельном Compose-проекте.

Описание типов метрик и Go-клиента есть в [документации Prometheus](https://prometheus.io/docs/guides/go-application/), описание JSON-логгера — в [документации slog](https://pkg.go.dev/log/slog).
