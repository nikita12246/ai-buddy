# SDD

Перед реализацией уточняем поведение, затем задачи, затем код и тесты. Это исходные критерии, не отчёт о готовности.

Черновик [спецификации AI-модуля](ai-behavior.md) описывает входы, ответ, ошибки и примеры для согласования с backend и данными.

| ID | Критерий | Проверка |
| --- | --- | --- |
| ACCESS-01 | Подтверждение валидной версии своего черновика создаёт одну операцию | [Интеграция API](../backend/internal/httpapi/onboarding_integration_test.go) |
| ACCESS-02 | Два конкурентных подтверждения дают один внешний ID | [Интеграция API](../backend/internal/httpapi/onboarding_integration_test.go) |
| ACCESS-03 | Потеря ответа после создания восстанавливается по тому же ключу | [Интеграция доставки](../backend/internal/httpapi/delivery_integration_test.go) |
| ACCESS-04 | Старая версия черновика не может быть подтверждена | [Интеграция API](../backend/internal/httpapi/onboarding_integration_test.go) |
| ACL-01 | Пользователь не читает чужие заявки/диалоги/черновики | [Интеграция API](../backend/internal/httpapi/onboarding_integration_test.go) |
| ACL-02 | Чужой документ не попадает в поиск, фактический LLM payload и источники | [Поиск по корпусу](../backend/internal/knowledge/corpus_test.go) и [интеграция AI](../backend/internal/httpapi/grounded_integration_test.go) |
| AI-01 | При отсутствии источника помощник уточняет или сообщает ограничение | [Ответ AI без источника](../backend/internal/ai/claude_test.go) и [поиск связанного материала](../backend/internal/knowledge/unanswered_test.go) |
| AI-02 | Прямая/косвенная injection не приводит к отправке заявки | [Интеграция AI](../backend/internal/httpapi/grounded_integration_test.go) |
| UI-01 | После reload восстановлены сообщения и состояние операции | [Тесты React](../frontend/src/app/App.test.tsx) и [сценарий Chromium](../frontend/e2e/access.e2e.ts) |
| UI-02 | Стажёр может изучить памятку по ревью и сохранить отметку, пока заявка согласуется. После одобрения интерфейс открывает инструкцию первой задачи своей команды | [Сценарий Chromium](../frontend/e2e/access.e2e.ts) |
| DELIVERY-01 | Чистый запуск по README и сохранность данных после перезапуска | [Запуск Compose в CI](../.github/workflows/ci.yml) и [HTTP-прогон с перезапуском](../scripts/dev/backend_demo_smoke.py) |

В каждом PR указываем ID критериев и тесты. Подробные спецификации по направлениям добавляются до соответствующего кода.

Для UI-02 отметка о прочтении хранится в браузере отдельно для каждого учебного профиля. Одобрение заявки определяется серверным статусом, а инструкция первой задачи открывается через API документов с проверкой прав.
