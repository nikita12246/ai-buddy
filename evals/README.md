# Проверки AI и поиска

В [data-cases.json](data-cases.json) 30 эталонов для учебных инструкций. Они покрывают предметные ответы, уточнения, отсутствие ответа и запросы, проверяющие права и устойчивость к инъекциям. В [starter-cases.json](starter-cases.json) лежит минимальный набор для раннера.

## Проверка без вызова модели

Из корня проекта:

```bash
python3 evals/prepare_reference.py --data-root . --output data/processed/ai-reference.json
(cd backend && go run ./cmd/ai-eval -check -cases ../data/processed/ai-reference.json)
```

Раннер проверяет формат эталонов в CI. Проверки API используют детерминированный клиент для сценариев с правами, повторами и ошибками модели.

Для поиска подготовьте PostgreSQL и загрузите корпус по [инструкции импорта](../scripts/ingest/README.md#сборка-общего-корпуса). Выполните из `backend`:

```bash
KNOWLEDGE_TEST_DATABASE_URL="$DATABASE_URL" go test ./internal/knowledge -run TestCorpus -count=1 -v
```

## Проверка с ModelHub

Задайте `LLM_PROVIDER`, `LLM_MODEL` и `LLM_API_KEY` в окружении. Из `backend` запустите:

```bash
go run ./cmd/ai-eval -review -cases ../data/processed/ai-reference.json > ../data/processed/ai-report.json
```

Этот запуск делает реальные запросы к модели. Раннер проверяет форму ответа и ссылки. Для каждого ответа вручную сверяют ожидаемые факты, допустимые источники и правильность уточнения или отказа. Карточки для сверки создаёт `python3 scripts/ingest/review_answers.py --report data/processed/ai-report.json` из корня проекта.

Для сравнения моделей используйте один исходный коммит и набор случаев. Сводку по задержке и токенам строит `python3 evals/compare_reports.py --help`. Отчёты с ответами моделей сохраняйте локально.
