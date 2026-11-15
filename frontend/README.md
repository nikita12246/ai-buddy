# AI-buddy Workspace — frontend

Frontend рабочего пространства для стажёров. React и TypeScript.

## Запуск

Из корня репозитория:

```bash
cd frontend
npm ci
npm run dev
```

Открой адрес, выведенный Vite (обычно `http://127.0.0.1:5173/`).
Из корня проекта запусти `python scripts/dev/backend_demo.py up --smoke`. Готовый интерфейс доступен на порту 8080. Для разработки Vite проксирует `/api` на backend на порту 8081. Браузер автоматически получает гостевую сессию Atlas.

В командном CI используется версия Node.js, указанная в корневом `.nvmrc`.

## Проверки

```bash
npm run check
npm test
npm run build
```

После изменения зависимостей нужно обновить и закоммитить `package-lock.json` вместе с `package.json`.

## Структура

- `src/app/components/DashboardOverview.tsx` — главная
- `src/app/components/AIAssistant.tsx` — диалог
- `src/app/components/KnowledgeBase.tsx` — база знаний
- `src/app/components/AccessRequests.tsx` — доступы
- `src/app/components/ProjectLinks.tsx` — связи проектов
- `src/app/components/Internship.tsx` — стажировка
- `src/app/components/DashboardLayout.tsx` — оболочка и навигация
- `src/app/data/` — демонстрационные данные
- `src/app/api/onboarding.ts` — клиент API учебных сессий, диалогов, источников и заявок
- `src/api.ts` — проверка командного API `/api/health`

## Текущее поведение и ограничения

- Чат использует поиск backend по PostgreSQL и открывает цитаты через API с проверкой доступа.
- Подтверждение черновика создаёт заявку в учебном сервисе. Статус обновляется из API.
- После перезагрузки в том же браузере восстанавливается последний диалог пользователя.
- Главная, база знаний и инструменты команды получают каталог из `GET /api/documents`. Сервер показывает только доступные текущей роли документы.
- План стажировки и задачи отображают учебный сценарий. Статус заявки приходит с backend.
- Реализованные сценарии и ограничения перечислены в [SPEC.md](SPEC.md).

## Интеграция

В репозитории предусмотрены `Dockerfile`, `nginx.conf` и проверка API. Контейнер отдаёт приложение через nginx и проксирует `/api` на `backend:8081`.

Лицензии сторонних компонентов: [ATTRIBUTIONS.md](ATTRIBUTIONS.md).
