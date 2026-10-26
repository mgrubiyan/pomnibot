# Помнибот

Бот и мини-приложение для создания учебных карточек из конспектов и интервального повторения.

[![CI](https://github.com/mgrubiyan/pomnibot/actions/workflows/ci-cd.yml/badge.svg)](https://github.com/mgrubiyan/pomnibot/actions/workflows/ci-cd.yml)
[![Веб-приложение](https://img.shields.io/badge/%D0%92%D0%B5%D0%B1--%D0%BF%D1%80%D0%B8%D0%BB%D0%BE%D0%B6%D0%B5%D0%BD%D0%B8%D0%B5-2563eb)](https://pomnibot.pslu.ru)
[![MAX](https://img.shields.io/badge/MAX-6C3BF5)](https://max.ru/t583_hakaton_max_bot)

## Генерация и распознавание

Генерация карточек — OpenRouter, GigaChat и Qwen через Yandex AI Studio.
Распознавание документов — Yandex OCR.

Настройки — в [`.env.example`](.env.example).

## Запуск

Понадобятся Docker и Docker Compose.

```bash
cp .env.example .env
docker compose up --build -d
```

Приложение: [http://localhost:8080](http://localhost:8080).

По умолчанию используется локальный режим; для ботов и генерации нужны `APP_ENV=prod` и ключи в `.env`.

Остановка:

```bash
docker compose down
```

## Тесты

```bash
task test               # Backend и frontend unit-тесты, без Docker
task test:integration   # PostgreSQL 18 в одноразовых контейнерах, нужен Docker
task test:race          # Backend unit-тесты с race detector
task lint               # Frontend и backend, включая integration-код
task bench              # Go-бенчмарки, время и аллокации
```

Интеграционные тесты помечены `//go:build integration`. Имя
`*_integration_test.go` само по себе не исключает файл из обычного `go test`.
CI запускает unit- и integration-тесты с race detector, а также perf-smoke.
Для PDF-тестов нужны утилиты Poppler (`pdftoppm` и `pdftotext`).

### Нагрузочные сценарии

Полный прогон можно запустить в GitHub Actions: **API performance → Run workflow**.
Выберите ветку, группу сценариев, режим и длительность ступени. Отчёты доступны
в artifacts запуска 14 дней; в git они не сохраняются.

Локально:

```bash
task perf:smoke
task perf -- -only study
task perf -- -only rate -mode explore
task perf -- -only study -out ../artifacts/perf/study.json
```

По умолчанию файлы не создаются. Путь `-out` отсчитывается от `backend/`;
директории создаются автоматически. Для каждого запуска используйте отдельное
имя отчёта. `/artifacts/` исключён из git. В `docs` результаты не записываются.
При сохранении JSON обновляется после каждой ступени, а в конце содержит также
ошибку запуска, если она возникла.

- `check` (по умолчанию): нарушение критериев любой выполненной ступени даёт
  ненулевой код завершения. Более высокие ступени провалившейся группы пропускаются;
  обе ступени study выполняются для оценки запаса.
- `explore`: насыщение concurrency/rate и провал study на 100 пользователей
  остаются результатом измерения. Study на 50 пользователей, smoke, ошибки
  инфраструктуры и нарушения целостности данных всегда приводят к ошибке.
- `perf:smoke`: два пользователя, две секунды, пауза 100 мс между ответами.
  Проверяет авторизацию MAX/Telegram, чтение, запись ответов и сохранённый прогресс.

Стенд использует PostgreSQL 18, миграции и HTTP router приложения. Реальные
LLM и внешние API не вызываются: ответы на подготовленные вопросы проверяются
локально. Записи используют текущий `/answer`, а flip-карточки — также `/reveal`.
Заданная частота в rate — число пользовательских действий в секунду: действие
ответа делает несколько HTTP-запросов. Фактический HTTP RPS измеряется отдельно.
Пороги: отсутствие ошибок, p95 ≤ 500 мс, p99 ≤ 1000 мс по каждой операции;
для rate также не допускаются пропущенные слоты планировщика.
Study/concurrency проверяют вход и постоянную нагрузку, rate — постоянную
нагрузку; метрики входа для rate сохраняются как диагностика.

API, PostgreSQL и генератор нагрузки работают на одной машине. Используйте
сопоставимое окружение и отдельный запуск для сравнения производительности;
цифры общего GitHub runner не являются оценкой ёмкости production.

Расчёт метрик, правила завершения и обработка ошибок проверяются обычными
unit-тестами в `backend/internal/perftest`, без Docker. CLI в `cmd/perf`
отвечает за параметры, сигналы завершения и необязательное сохранение отчёта.
