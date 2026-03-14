# PicStore

Сервис для хранения и обмена изображениями с возможностью загрузки через файл или URL, тегированием и управлением видимостью.

## Оглавление

- [Возможности](#возможности)
- [Технологии](#технологии)
- [Архитектура](#архитектура)
- [Установка и запуск](#установка-и-запуск)
  - [Требования](#требования)
  - [Настройка окружения](#настройка-окружения)
  - [Запуск с Docker Compose](#запуск-с-docker-compose)
  - [Запуск вручную](#запуск-вручную)
- [Использование API](#использование-api)
- [Структура проекта](#структура-проекта)
- [Лицензия](#лицензия)

## Возможности

- **Загрузка изображений**:
  - Через файл (multipart/form-data)
  - Через URL (скачивание по ссылке)
- **Управление видимостью**: публичные или приватные изображения
- **Тегирование**: добавление тегов к изображениям для организации
- **Кэширование**: Redis для ускорения доступа к спискам изображений
- **Аутентификация**: интеграция с SSO (single-auth)
- **Пагинация и навигация** по публичным изображениям
- **REST API** с документацией в коде
- **Веб-интерфейс** для просмотра и загрузки

## Технологии

- **Backend**: Go 1.25, Gin (веб-фреймворк)
- **База данных**: PostgreSQL (хранение метаданных изображений)
- **Кэш**: Redis
- **Аутентификация**: single-auth (JWT, куки)
- **Логирование**: zap через logger от single-auth
- **Конфигурация**: env-переменные, godotenv
- **Контейнеризация**: Docker, Docker Compose
- **Фронтенд**: HTML/CSS/JS, шаблоны Go (templates)

## Архитектура

Проект построен по принципам чистой архитектуры (Clean Architecture) с разделением на слои:

- **`internal/core`** — бизнес-логика (picstore, auth)
- **`internal/adapters`** — адаптеры для внешних систем:
  - `api/rest` — HTTP-обработчики и middleware
  - `storage` — работа с БД (PostgreSQL) и кэшем (Redis)
  - `models` — сущности БД
  - `config` — конфигурация
- **`cmd/main`** — точка входа, инициализация и запуск сервера
- **`static`** — статические файлы (CSS, JS)
- **`templates`** — HTML-шаблоны

## Установка и запуск

### Требования

- Docker и Docker Compose (рекомендуемый способ)
- Или Go 1.25+, PostgreSQL 18+, Redis 7+

### Настройка окружения

Скопируйте пример переменных окружения и настройте под свои нужды:

```bash
cp .env.example .env
```

Отредактируйте `.env` (см. раздел [Конфигурация](#конфигурация)).

### Запуск с Docker Compose

Самый простой способ развернуть весь стек (сервер, БД, Redis):

```bash
docker-compose up -d
```

Сервис будет доступен по адресу `http://localhost:8080`.

### Запуск вручную

1. Установите зависимости Go:

   ```bash
   go mod download
   ```

2. Запустите PostgreSQL и Redis (например, через docker-compose только для инфраструктуры).

3. Выполните миграции БД (если требуется) — в текущей версии таблицы создаются автоматически через GORM AutoMigrate.

4. Запустите сервер:

   ```bash
   go run cmd/main/main.go
   ```

Или используйте Makefile:

```bash
make run
```

## Конфигурация

Основные переменные окружения (полный список см. в `internal/adapters/config/config.go`):

| Переменная | Описание | Пример |
|------------|----------|--------|
| `SERVER_ADDRESS` | Адрес HTTP-сервера | `:8080` |
| `SERVER_BASEURL` | Базовый URL для генерации ссылок | `http://localhost:8080` |
| `DATABASE_ADDRESS` | DSN PostgreSQL | `postgres://user:pass@localhost:5432/db` |
| `REDIS_ADDRESS` | Адрес Redis | `localhost:6379` |
| `REDIS_PASSWORD` | Пароль Redis | `""` |
| `REDIS_DB` | Номер БД Redis | `0` |
| `LOG_LEVEL` | Уровень логирования | `info` |
| `LOG_DIR` | Директория для логов | `./logs` |
| `AUTH_SECRET_KEY` | Секретный ключ для подписи JWT | `secret` |
| `COOKIE_DOMAIN` | Домен для кук | `localhost` |
| `COOKIE_LIFETIME` | Время жизни кук | `24h` |
| `PIC_PATH` | Путь для хранения загруженных изображений | `./data/pictures` |

## Использование API

### Эндпоинты

- `GET /` — главная страница со списком публичных изображений
- `GET /upload` — форма загрузки изображения
- `POST /upload` — загрузка изображения (файл или URL)
- `GET /image/{path}` — получение изображения по пути
- `GET /profile` — профиль пользователя (требуется аутентификация)
- `GET /login` — вход через SSO
- `GET /logout` — выход

Подробнее см. `internal/adapters/api/rest/handlers.go`.

### Пример загрузки через curl

```bash
curl -X POST -F "file=@/path/to/image.jpg" -F "isPublic=true" -F "tags=природа лето" http://localhost:8080/upload
```

## Структура проекта

```
picstore/
├── cmd/main/main.go          # Точка входа
├── internal/
│   ├── adapters/
│   │   ├── api/rest/         # HTTP-обработчики, middleware, конфиг
│   │   ├── config/           # Конфигурация приложения
│   │   ├── models/           # Модели БД (GORM)
│   │   └── storage/          # Работа с хранилищами (БД, Redis)
│   ├── core/
│   │   ├── picstore/         # Бизнес-логика работы с изображениями
│   │   └── auth/             # Логика аутентификации
├── static/                   # CSS, JS
├── templates/                # HTML-шаблоны
├── docker-compose.yml        # Docker Compose конфигурация
├── Dockerfile                # Образ приложения
├── Makefile                  # Утилиты для разработки
├── go.mod, go.sum            # Зависимости Go
├── LICENSE                   # Лицензия MIT
└── README.md                 # Этот файл
```

## Разработка

### Команды Makefile

- `make run` — запустить сервер в development-режиме (требует запущенных PostgreSQL и Redis)
- `make up-store` — запустить только базу данных и Redis через Docker Compose
- `make down` — остановить Docker Compose сервисы
- `make reg` — собрать Docker-образ и отправить в приватный registry (требует настройки)

### Создание .env файла

Пример `.env` файла (скопируйте и настройте под свои нужды):

```env
LOG_LEVEL=info
LOG_DIR=./logs
SERVER_BASEURL=http://localhost:8080
SERVER_ADDRESS=:8080
AUTH_SECRET_KEY=your-secret-key
COOKIE_DOMAIN=localhost
COOKIE_SECURE=false
COOKIE_LIFETIME=24h
DATABASE_ADDRESS=postgres://user:pass@localhost:5432/picstore
REDIS_ADDRESS=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0
PIC_PATH=./data/pictures
```

### Тестирование

На данный момент проект не включает unit-тесты. Для проверки работоспособности используйте ручные тесты через веб-интерфейс или curl.

## Лицензия

Проект распространяется под лицензией MIT. Полный текст лицензии доступен в файле [LICENSE](LICENSE).