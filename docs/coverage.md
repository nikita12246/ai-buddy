# Покрытие тестами

Результат CI финального исходного снимка `aaf7a79`. В публичном репозитории код приложения и тесты взяты из этого снимка.

| Код | Покрыто строк | Учтено строк | Покрытие |
| --- | ---: | ---: | ---: |
| Backend Go, включая учебный сервис | 1174 | 1613 | 72,78% |
| Frontend TypeScript и React | 1388 | 1530 | 90,72% |
| Всего | 2562 | 3143 | **81,51%** |

Для Go подсчёт использует диапазоны инструментированных блоков с операторами. Все строки многострочного блока учитываются вместе. Для frontend используются записи DA из LCOV.

## Воспроизведение

[GitHub Actions](https://github.com/nikita12246/ai-buddy/actions) запускает тесты на PostgreSQL и публикует артефакт `combined-line-coverage`. Для локального запуска подставьте URL отдельной тестовой PostgreSQL в `BACKEND_TEST_DATABASE_URL` и выполните из корня проекта:

```bash
(cd backend && go test -race -coverpkg=./... -coverprofile=coverage.out ./...)
(cd frontend && npm ci && npm run coverage)
python3 scripts/coverage_report.py backend/coverage.out frontend/coverage/lcov.info
```
