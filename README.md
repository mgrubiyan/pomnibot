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
