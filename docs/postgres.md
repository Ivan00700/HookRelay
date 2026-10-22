# PostgreSQL

Docker Compose запускает PostgreSQL 17 отдельно от HTTP-сервиса. Имена пользователя и базы — `hookrelay`. Команда `dbcheck` использует `pgx` для проверки соединения. HTTP API работает с `MemoryStore`; создание таблиц само по себе не меняет способ хранения данных API.

## Запуск базы

Требуются Docker Desktop с Linux-контейнерами и Docker Compose.

При первом запуске скопируйте `.env.example` в `.env` и задайте свой пароль. Если `.env` уже существует, сохраните его значения. Пример содержимого:

```dotenv
POSTGRES_PASSWORD=replace-with-a-local-password
POSTGRES_PORT=5433
```

`.env` исключён из Git. Compose читает его автоматически при запуске из корня проекта.

```powershell
docker compose up -d --wait db
docker compose ps
```

Ожидаемый статус — `healthy`. Адрес для программ на компьютере: `127.0.0.1:5433`. Внутри контейнера PostgreSQL слушает `5432`; публикация порта соединяет эти два адреса. Другой внешний порт можно задать через `POSTGRES_PORT`.

Данные расположены в именованном томе `postgres_data`. `docker compose down` удаляет контейнер и сеть, сохраняя этот том. Флаг `-v` удаляет том вместе с данными. Смена внешнего порта не изменяет содержимое тома. Значения `POSTGRES_USER`, `POSTGRES_DB` и `POSTGRES_PASSWORD` инициализируют пустой том; изменение пароля в `.env` не меняет пароль уже созданного пользователя БД.

## Таблицы и SQL-файлы

| Файл | Таблица | Назначение |
| --- | --- | --- |
| `001_init.sql` | `endpoints` | Адреса получателей и их секреты |
| `002_create_events.sql` | `events` | События, JSON-тела и статусы |
| `003_create_delivery_attempts.sql` | `delivery_attempts` | Номера попыток, время выполнения, HTTP-коды и ошибки |

SQL-файлы применяются вручную, по порядку и один раз для каждой базы. Автоматического учёта применённых миграций в проекте нет. Повторный `CREATE TABLE` сообщает об уже существующей таблице.

Для пустой базы выполните все три файла:

```powershell
Get-Content -Raw migrations/001_init.sql,migrations/002_create_events.sql,migrations/003_create_delivery_attempts.sql | docker compose exec -T db psql -U hookrelay -d hookrelay -v ON_ERROR_STOP=1 --single-transaction -f -
```

`ON_ERROR_STOP=1` останавливает выполнение при ошибке. `--single-transaction` применяет весь переданный SQL в одной транзакции: при ошибке изменения этой команды откатываются. `-f -` читает SQL из стандартного ввода.

Если первые два файла уже применены, выполните только третий:

```powershell
Get-Content -Raw migrations/003_create_delivery_attempts.sql | docker compose exec -T db psql -U hookrelay -d hookrelay -v ON_ERROR_STOP=1 --single-transaction -f -
```

Таблица `events` ссылается на `endpoints`, а `delivery_attempts` — на `events`. `ON DELETE RESTRICT` запрещает удалять запись, на которую ссылается история. Для события уникальна пара `(event_id, attempt_number)`; время завершения и результат запроса могут отсутствовать.

## DBeaver

Создайте подключение PostgreSQL со следующими значениями:

| Параметр | Значение |
| --- | --- |
| Host | `127.0.0.1` |
| Port | `5433` или значение `POSTGRES_PORT` |
| Database | `hookrelay` |
| User | `hookrelay` |
| Password | Значение `POSTGRES_PASSWORD` из `.env` |

Для подключения, настроенного на порт `5432`, измените Port на `5433` и переподключитесь. Таблицы находятся в схеме `public`. Для просмотра структуры обновите список таблиц после применения SQL-файлов.

## Проверка соединения из Go

Команда читает строку подключения из `DATABASE_URL`. Go не загружает `.env` автоматически. В PowerShell задайте переменную для текущего терминала, подставив пароль из `.env`:

```powershell
$databasePassword = [uri]::EscapeDataString('пароль из POSTGRES_PASSWORD')
$env:DATABASE_URL = "postgres://hookrelay:${databasePassword}@127.0.0.1:5433/hookrelay?sslmode=disable"
go run ./cmd/dbcheck
```

`EscapeDataString` кодирует специальные символы пароля для URL. При изменении `POSTGRES_PORT` подставьте тот же порт в строку подключения. `sslmode=disable` используется для локальной базы в этом Compose-окружении.

Успешный запуск выводит имя базы, пользователя и версию PostgreSQL. Команда ограничивает проверку пятью секундами; при ошибке завершается с ненулевым кодом. Ошибки разбора строки подключения выводятся без её содержимого.

Пул соединений создаётся в `internal/postgres.Open`. Вызов `Ping` проверяет реальное соединение с сервером. Если проверка не удалась, функция закрывает пул; после успешного открытия за закрытие отвечает вызывающий код.

Для интеграционного теста используйте ту же локальную базу:

```powershell
$env:TEST_DATABASE_URL = $env:DATABASE_URL
go test ./internal/postgres -run TestOpenIntegration -v
```

Тест выполняет `SELECT 1` и не изменяет данные. Удалить переменные из текущего терминала можно так:

```powershell
Remove-Item Env:DATABASE_URL,Env:TEST_DATABASE_URL -ErrorAction SilentlyContinue
```
