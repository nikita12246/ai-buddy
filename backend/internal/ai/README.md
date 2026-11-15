# AI

Модуль реализует `Client.Generate`. Backend передаёт вопрос, разрешённые фрагменты и каталог, затем получает ответ, ID источников, уточнение или предложение черновика. Границы описаны в [спецификации](../../../specs/ai-behavior.md).

`NewClient` выбирает адаптер по `LLM_PROVIDER`. Для `modelhub` запрос идёт на `https://modelhub.my/v1/chat/completions`, для `anthropic` используется Anthropic Messages API. Ключ следует выбирать для указанного провайдера. Оба адаптера проверяют выходные поля на сервере. Системная инструкция хранится в [system-prompt.md](system-prompt.md), схема — в [response-schema.json](response-schema.json). Тесты подменяют HTTP-транспорт и запускаются через `go test ./internal/ai` из папки `backend`.

## Пробный вызов Sonnet 5.5 через ModelHub

Для вызова нужен ключ [ModelHub](https://modelhub.my/) с доступом к модели. В PowerShell 7 из корня проекта:

```powershell
$env:LLM_PROVIDER = 'modelhub'
$env:LLM_MODEL = 'claude-sonnet-5-5'
$env:LLM_API_KEY = Read-Host 'ModelHub API key' -MaskInput
Push-Location backend
try { go run ./cmd/ai-smoke } finally { Pop-Location; Remove-Item Env:\LLM_API_KEY }
```

Вызов отправляет одну учебную инструкцию и возвращает проверенный JSON. Для обычного запуска Compose локальные значения можно хранить в `.env`. Команда `ai-smoke` читает только окружение процесса, поэтому при запуске Go из PowerShell ключ нужно передать как в примере. API-ключи остаются у владельца аккаунта. Для CI и локальных автоматических тестов ключ не нужен.
