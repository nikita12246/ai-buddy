# Контракты API

`GET /api/health` возвращает `{"status":"ok","service":"ai-buddy-api"}`. Он проверяет HTTP-процесс. Сессии, диалог, черновик и операции реализованы по [спецификации backend](../specs/backend-session-drafts.md).

## Поиск: knowledge → AI

Вход: actor из серверной сессии, query, ограниченный top-k. Выход: массив `{chunk_id, document_id, title, text, source_url, version}`. ACL применяется до ранжирования. Пустая выдача — нормальный результат. Реальные источники не назначаются моделью.

## Ответ: AI → backend

`{answer, source_ids, clarifying_question, draft_proposal}`. Последние два поля могут быть null. `draft_proposal`: `{tool_id, resource_id, justification}`. Проверяются типы, длины, ID каталога и принадлежность source IDs фактически найденным фрагментам. Actor и статус заявки не задаются моделью.

Для стыковки `knowledge.Searcher.Search(ctx, actor, query, limit)` получает команду и роль из серверной сессии. Из результата backend передаёт в `ai.Input.Chunks` только `ChunkID` как `ID`, `Title` и `Text`. `DocumentID`, `SourceURL` и `Version` остаются у backend для отображения источников по `source_ids`. Для прямого открытия документа backend повторно проверяет права. Каталог команды фильтруется сервером. В `ai.Input.Catalog` передаются `ToolID`, `ResourceID` и `Title` как `Name`.

Backend может создать клиент через `ai.NewClient` и вызвать `Generate` после сохранения сообщения. При сбое AI пользовательское сообщение остаётся доступным для повтора. Общий дедлайн HTTP запроса должен оставлять запас на поиск, AI и сохранение ответа. Состояние заявки и следующий шаг берутся из сервиса заявок. `draft_proposal` остаётся предложением до серверной проверки и отдельного подтверждения.

## HTTP API сценария

| Метод и путь | Вход / выход |
| --- | --- |
| GET /api/me | Профиль из сессии |
| POST /api/conversations | Создать диалог |
| POST /api/conversations/{id}/messages | client_message_id, text → ответ/уточнение |
| GET /api/conversations/{id} | messages, draft, operation, необязательный pending_message с client_message_id и text |
| PATCH /api/drafts/{id} | expected_version и поля → новая версия |
| POST /api/drafts/{id}/confirm | version → существующая/новая operation |
| GET /api/operations | {operations} со статусами подтверждённых заявок и идентификаторами ресурсов текущего пользователя |
| GET /api/operations/{id} | delivery_state, external_id, service_status, next_step |
| GET /api/documents/{id} | Источник после повторной проверки ACL |

Ошибка: `{error: {code, message}, request_id}`. Не возвращать содержимое чужих объектов. Устаревшая версия — 409; ошибочные поля — 422; неизвестный исход внешней отправки — состояние reconciling, а не сообщение об отказе.

## Сервис доступа: backend → mock

`CreateRequest(key, immutable_payload)`, `LookupByKey(key)`, `GetStatus(external_id)`. Ключ и хеш тела сохраняются атомарно с созданием заявки. Повтор с тем же телом возвращает тот же ID, другой payload под тем же ключом — конфликт.

Доставка: queued → sending → delivered; неизвестный исход → reconciling. Бизнес-статус: pending_approval → approved/rejected. `approved` означает выданный учебный доступ только в нашем явно описанном mock-контракте.

[HTTP-контракт учебного сервиса и восстановление доставки](../specs/backend-access-delivery.md).
