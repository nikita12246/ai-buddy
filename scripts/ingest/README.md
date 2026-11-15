# Импорт чек-листа стажёра

Первая версия импортирует две вкладки предоставленной Google-таблицы из CSV. Одна непустая строка таблицы становится одним поисковым фрагментом. Сохраняются формулировки, сроки, вкладка, номер строки и ссылка на неё. У каждой вкладки своя версия по SHA-256 исходного CSV. Область доступа — `public`, тип источника — `avito_public`.

Загрузите оригинальные вкладки из реестра `data/sources.json` в `data/raw/avito-checklist/general.csv` и `data/raw/avito-checklist/backend.csv`. Экспорт Google Sheets можно повторить из корня репозитория.

```bash
mkdir -p data/raw/avito-checklist
curl --fail --location 'https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0/export?format=csv&gid=1073060303' -o data/raw/avito-checklist/general.csv
curl --fail --location 'https://docs.google.com/spreadsheets/d/1qKFqOzfSZ2jEz57jBSmq7rukDXi_tRfWohgemViceV0/export?format=csv&gid=0' -o data/raw/avito-checklist/backend.csv
python3 scripts/ingest/checklist.py
```

Импорт работает локально на Python 3.10+ без сторонних зависимостей. При необходимости передайте `--input-dir` и `--output`. По умолчанию результат записывается в `data/processed/avito-checklist.jsonl`. Повторный запуск на тех же файлах даёт тот же результат. Ошибка чтения или неожиданная структура вкладки прерывают импорт до записи нового результата.

В каждой записи есть `chunk_id`, `document_id`, `title`, `text`, `source_url`, `version`, `source_id`, `sheet`, `row`, `period`, `scope` и `source_kind`. JSONL используется при сборке общего корпуса. Инструкция загрузки в PostgreSQL приведена ниже.

## Сборка общего корпуса

После получения двух CSV добавьте зафиксированные разделы Playbook и снимок статьи о первых днях стажировки. Карточка Zero Bug Policy уже в репозитории.

```bash
mkdir -p data/raw/playbook data/raw/habr
curl --fail --location 'https://raw.githubusercontent.com/avito-tech/playbook/0c1d2aa1501ca6889357908f54b12695f0534e48/development-principles.md' -o data/raw/playbook/development-principles.md
curl --fail --location 'https://raw.githubusercontent.com/avito-tech/playbook/0c1d2aa1501ca6889357908f54b12695f0534e48/avito-paas.md' -o data/raw/playbook/avito-paas.md
curl --fail --location 'https://raw.githubusercontent.com/avito-tech/playbook/0c1d2aa1501ca6889357908f54b12695f0534e48/spt.md' -o data/raw/playbook/spt.md
curl --fail --location 'https://habr.com/ru/amp/publications/550246/' -o data/raw/habr/first-days.html
python3 -m scripts.ingest.corpus
python3 -m scripts.ingest.seed > data/processed/knowledge-seed.sql
```

Корпус объединяет чек-лист, выбранные публичные документы из `data/public-documents.json` и учебные политики. Для дополнительных локальных HTML-снимков укажите `kind: "html"` в манифесте. По умолчанию извлечение выбирает `article` или `main`. Поле `content_class` позволяет выбрать точный контейнер, `headings` — конкретные разделы. Отсутствие обязательного раздела завершает сборку с ошибкой. Карточки с собственным пересказом хранятся в `data/public-notes/` с типом `summary` и ссылкой на оригинал. Каждый новый источник добавляется в реестр.

Загрузите корпус в локальную PostgreSQL после задания строки подключения `DATABASE_URL`.

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/internal/knowledge/schema.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f data/processed/knowledge-seed.sql
```

Загрузка обновляет фрагменты по ID и удаляет устаревшие строки включённых в снимок источников в одной транзакции. Повторный запуск не создаёт дублей. Поиск описан в [документации модуля](../../backend/internal/knowledge/README.md).

## Повторение зафиксированного снимка

Состав и SHA-256 текущего снимка хранятся в [`data/corpus-lock.json`](../../data/corpus-lock.json). В чистой копии репозитория положите шесть исходных файлов в `data/raw` по указанным в нём путям. Можно также передать путь к отдельной папке со снимками. Три Markdown-файла загружаются из зафиксированного коммита по командам выше. Для точного повторения используйте сохранённые CSV и HTML. Их свежие загрузки могут иметь другое содержимое.

```bash
python3 -m scripts.ingest.build --raw-dir /path/to/raw-snapshots
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f backend/internal/knowledge/schema.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f data/processed/knowledge-seed.sql
```

Сборка сверяет SHA-256 каждого файла, проверяет число фрагментов и хеш выходного корпуса, затем записывает `corpus.jsonl` и `knowledge-seed.sql`. Если снимок изменился, используйте команды импорта из предыдущего раздела, проверьте выдачу и обновите фиксированные хеши отдельным коммитом. При удалении всего источника из нового манифеста его старые строки нужно удалить явно либо пересобрать отдельную базу знаний с нуля. Текущий seed удаляет устаревшие строки только по включённым в него `source_id`.

Тип `vendor_docs` отведён для справки производителей. При обновлении существующей базы сначала выполните `schema.sql`, который расширяет ограничение на `source_kind`. Для пользовательской выдачи сохраняйте различие между `avito_public`, `demo_policy` и `vendor_docs`.
