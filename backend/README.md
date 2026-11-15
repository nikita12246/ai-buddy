# Backend AI-buddy

Backend хранит учебные сессии, диалоги, черновики и очередь заявок в PostgreSQL. Отдельный процесс mock-access принимает заявки по HTTP. Поведение API описано в [спецификации](../specs/backend-session-drafts.md). Для тестового черновика отправьте «Нужен доступ к учебному GitLab», добавьте обоснование через PATCH и подтвердите полученную версию. Ответ помечен как учебный. `queued` означает, что операция сохранена и ждёт отправки.

## Демо одной командой

Запустите Docker Desktop или Docker Engine. Из корня репозитория выполните:

```bash
python3 scripts/dev/backend_demo.py up --smoke
```

Команда запускает отдельный проект `ai-buddy-backend-demo` с интерфейсом, PostgreSQL, API и учебным сервисом заявок. Она применяет миграции, загружает учебные политики и публичные карточки из Git. Проверка проходит через прокси интерфейса от входа и цитаты из PostgreSQL до `approved`, включая права Atlas и Orbit и повтор подтверждения. Ключ модели и архив исходников для этого запуска не нужны.

Интерфейс доступен на `http://127.0.0.1:8080`, API — на порту 8081, сервис заявок — на 8082. При занятом порте задайте `BACKEND_DEMO_WEB_PORT`, `BACKEND_DEMO_API_PORT` или `BACKEND_DEMO_MOCK_PORT`. Интерфейс открывается автоматически с гостевой cookie-сессией. Токены Atlas и Orbit используются в HTTP-проверках. Сохраните `.env.backend-demo` для повторных запусков с тем же томом PostgreSQL.

```bash
python3 scripts/dev/backend_demo.py smoke
python3 scripts/dev/backend_demo.py logs
python3 scripts/dev/backend_demo.py down
```

`down` останавливает проект и сохраняет том PostgreSQL. При повторном `up` диалоги и заявки остаются, поисковый индекс обновляется из выбранного снимка.

Для полной базы используйте `up --full` после подготовки папки `data/raw`. Можно передать `--raw-dir /path/to/raw`. Для вызова живой модели задайте `LLM_API_KEY` в окружении и запустите `up --full --live`. Проверка `--smoke` рассчитана на учебный ответ, её запускают без `--live`.

## Запуск без Compose

Нужны Go из `go.mod`, отдельная тестовая PostgreSQL и клиент `psql`. Из корня репозитория укажите `DATABASE_URL` через своё окружение и выполните:

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/001_session_drafts.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/002_demo_seed.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/003_operation_delivery.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/005_grounded_messages.sql
export DEMO_ATLAS_TOKEN="$(openssl rand -hex 32)"
export DEMO_ORBIT_TOKEN="$(openssl rand -hex 32)"
(cd backend && go run ./cmd/api)
```

API по умолчанию слушает `127.0.0.1:8081`. Запускайте его с этими переменными только для тестовых аккаунтов. На старте токены привязываются к аккаунтам Atlas и Orbit на 24 часа. В БД хранятся хеши. В запросе передайте `Authorization: Bearer` с токеном нужной сессии. Без `DATABASE_URL` процесс предоставляет только `/api/health`.

Для проверки профиля запустите из другого терминала с тем же токеном:

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer $DEMO_ATLAS_TOKEN" \
  http://127.0.0.1:8081/api/me
```

## Учебный сервис заявок

Задайте `MOCK_DATABASE_URL` для базы сервиса и примените схему. Для локального демо подходит одна тестовая база с отдельной схемой `mock_access`. API читает статусы через HTTP.

```bash
psql "$MOCK_DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/004_mock_access.sql
export MOCK_ACCESS_TOKEN="$(openssl rand -hex 32)"
export MOCK_DECISION_TOKEN="$(openssl rand -hex 32)"
(cd backend && go run ./cmd/mock-access)
```

По умолчанию сервис слушает `127.0.0.1:8082`. В терминале API задайте `MOCK_ACCESS_URL=http://127.0.0.1:8082` и тот же `MOCK_ACCESS_TOKEN`, затем запустите API. Без этой пары подтверждённые операции остаются в очереди. `MOCK_DECISION_TOKEN` хранится у оператора демо и в процесс API не передаётся.

### Решение оператора по учебной заявке

Скопируйте номер заявки со страницы «Доступы» и задайте его в терминале как `EXTERNAL_ID`. Если приложение поднято через скрипт демо, значение `MOCK_DECISION_TOKEN` возьмите из локального `.env.backend-demo`, где оно записано под именем `BACKEND_DEMO_DECISION_TOKEN`. Этот токен даёт право принять учебное решение от имени оператора.

При проверке через API внешний номер возвращает `GET /api/operations/{id}` в поле `external_id`. У доставленной заявки бизнес-статус сначала равен `pending_approval`. Для одобрения выполните:

```bash
curl --fail --silent --show-error -X PATCH \
  -H "Authorization: Bearer $MOCK_DECISION_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"status":"approved"}' \
  "http://127.0.0.1:8082/requests/$EXTERNAL_ID/decision"
```

API увидит решение при следующем опросе. Контракт повторов, аренды и восстановления описан в [спецификации доставки](../specs/backend-access-delivery.md).

## Поиск и модель

Из корня репозитория соберите базу знаний по [инструкции импорта](../scripts/ingest/README.md#повторение-зафиксированного-снимка). Загрузите её в ту же тестовую PostgreSQL и укажите путь к каталогу:

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/internal/knowledge/schema.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f data/processed/knowledge-seed.sql
export KNOWLEDGE_RESOURCES_PATH="$PWD/data/synthetic/resources.json"
export LLM_PROVIDER=modelhub
export LLM_MODEL=gpt-6.1-sol
(cd backend && go run ./cmd/api)
```

Задайте свой `LLM_API_KEY` в окружении до запуска API. Без ключа действует учебный ответ. При загруженном корпусе `GET /api/documents/{id}` доступен в обоих режимах. JSON сообщения содержит `source_ids` и `sources`. Открытие документа и чтение сохранённого ответа повторно проверяют источники. После ошибки провайдера повторите запрос с тем же `client_message_id` и текстом. Подробности в [спецификации диалога](../specs/backend-grounded-chat.md).

## Тест

Запустите из `backend` с URL отдельной тестовой БД. Тест применяет миграции, создаёт учебные сессии, проверяет ACL и повтор после ошибки модели, а также имитирует потерю ответа после записи заявки. Модель подменена детерминированным клиентом. Используйте пустую базу, чтобы воркер обрабатывал только тестовые операции.

```bash
BACKEND_TEST_DATABASE_URL="$DATABASE_URL" go test -race ./... -count=1
```

Интерфейс может подключить маршруты из [контракта API](../docs/CONTRACTS.md).
