# AI-buddy

Веб-помощник для стажёров. Находит инструкции команды, помогает оформить доступ к учебному GitLab и показывает статус заявки и следующий шаг до первой задачи.

Проект команды для кейса Авито на Hack&Change 2026.

[Презентация (PDF)](docs/AI-buddy-defense.pdf) · [Видеодемонстрация (MP4)](docs/AI-buddy-demo.mp4) · [Покрытие тестами](docs/coverage.md)

## Быстрый запуск

Понадобятся Docker с Compose v2 и Python 3.12+. Версии Go и Node.js зафиксированы в [go.mod](backend/go.mod) и [.nvmrc](.nvmrc), PostgreSQL 17 — в Compose. Для запуска в Docker достаточно Docker и Python.

```bash
git clone https://github.com/nikita12246/ai-buddy.git
cd ai-buddy
python3 scripts/dev/backend_demo.py up --smoke
```

Откройте [http://127.0.0.1:8080](http://127.0.0.1:8080). Браузер автоматически получит гостевую сессию стажёра Atlas. Команда запускает интерфейс, API, PostgreSQL и учебный сервис заявок, загружает базу знаний и проверяет основной сценарий. В учебном режиме ответы формируются локально из базы знаний.

Для HTTP-проверок скрипт генерирует токены Atlas и Orbit в локальном `.env.backend-demo`. Этот же файл содержит отдельный токен для согласования заявки оператором. Остановить приложение с сохранением учебных данных можно командой `python3 scripts/dev/backend_demo.py down`. Дополнительные команды и настройки портов есть в [руководстве backend](backend/README.md#демо-одной-командой).

## Сценарий для жюри

1. В чате отправьте «Нужен доступ к учебному GitLab». Откройте источник ответа и проверьте роль Reporter.
2. Откройте черновик, введите обоснование «Нужно прочитать README для первой задачи» и подтвердите заявку.
3. В разделе «Доступы» дождитесь номера заявки и статуса «На согласовании». Откройте памятку по code review и отметьте её прочитанной.
4. [Согласуйте заявку через учебный API](backend/README.md#учебный-сервис-заявок). Подставьте номер заявки в `EXTERNAL_ID`, а значение `BACKEND_DEMO_DECISION_TOKEN` из локального файла используйте как `MOCK_DECISION_TOKEN`.
5. Дождитесь статуса «Доступ предоставлен» и откройте инструкцию первой задачи. После перезагрузки страницы в том же браузере проверьте номер заявки и её статус.

## Режим с моделью

Скопируйте [пример конфигурации](.env.example) в `.env` и укажите свой ключ в `LLM_API_KEY`. Для ModelHub подходят `LLM_PROVIDER=modelhub` и `LLM_MODEL=claude-sonnet-5-5` либо `LLM_MODEL=gpt-6.1-sol` при наличии доступа к выбранной модели.

```bash
cp .env.example .env
# Заполните LLM_API_KEY в .env и выполните:
python3 scripts/dev/backend_demo.py up --live
```

Переменные окружения терминала имеют приоритет над файлом. Сообщения, отправленные модели, расходуют токены. В логах (`python3 scripts/dev/backend_demo.py logs`) ModelHub показывает имя модели и расход токенов, когда провайдер возвращает эти данные. Держите ключ в локальном окружении.

## Проверки

Из корня проекта запустите `python3 scripts/check_repo.py` и `python3 -m unittest discover -s scripts/tests`. Для Go используйте `go test -race ./...` и `go vet ./...` из `backend`. Интеграционные тесты Go требуют [отдельную тестовую PostgreSQL](backend/README.md#тест).

Из `frontend` выполните `npm ci`, `npm run check`, `npm test` и `npm run build`. Для сквозного теста сначала поднимите учебное демо, затем из `frontend` выполните `npx playwright install chromium` и `npm run test:e2e`.

[GitHub Actions](https://github.com/nikita12246/ai-buddy/actions) проверяет оба приложения, контейнеры и сценарий в Chromium, а также публикует воспроизводимый отчёт о покрытии. [Команды для расчёта покрытия](docs/coverage.md).

## Устройство проекта

- `frontend/` — React и TypeScript, чат, база знаний, заявки и этапы стажировки.
- `backend/` — Go API, поиск с учётом прав, очередь заявок и адаптер учебного сервиса.
- `data/` — [учебные данные и источники](data/README.md), `migrations/` — схема PostgreSQL.
- [Архитектура](ARCHITECTURE.md) · [SDD и критерии приёмки](specs/README.md) · [Проверки AI](evals/README.md) · [Атрибуция интерфейса](frontend/ATTRIBUTIONS.md).
