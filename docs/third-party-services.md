# Сторонние библиотеки и сервисы бэкенда

Список составлен по прямым зависимостям `backend/go.mod` и их использованию в исходном коде. Версии указаны из манифеста; транзитивные зависимости не перечислены отдельно.

## Библиотеки и фреймворки

| Библиотека | Версия | Для чего используется |
|---|---:|---|
| [Echo](https://github.com/labstack/echo) (`github.com/labstack/echo/v5`) | `v5.4.0` | HTTP-сервер и маршрутизация API. |
| [pgx](https://github.com/jackc/pgx) (`github.com/jackc/pgx/v5`) | `v5.11.0` | Подключение к PostgreSQL, пул соединений и драйвер для `database/sql`. |
| [go-redis](https://github.com/redis/go-redis) (`github.com/redis/go-redis/v9`) | `v9.23.0` | Клиент Redis. |
| [franz-go](https://github.com/twmb/franz-go) (`github.com/twmb/franz-go`) | `v1.22.2` | Клиент Kafka: producer и consumer. |
| `github.com/twmb/franz-go/pkg/kmsg` | `v1.15.0` | Типы и структуры Kafka protocol messages; используется с `franz-go`. |
| [Goose](https://github.com/pressly/goose) (`github.com/pressly/goose/v3`) | `v3.29.0` | Запуск миграций базы данных. |
| [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2) (`config`, `credentials`, `feature/s3/manager`, `service/s3`) | `v1.33.10`, `v1.20.10`, `v1.23.16`, `v1.114.5` | Настройка клиента, учётные данные и операции с S3-совместимым хранилищем, включая загрузку объектов. |
| `github.com/aws/smithy-go` | `v1.28.6` | Общая инфраструктура протоколов и ошибок AWS SDK. |
| [gopdf](https://github.com/signintech/gopdf) (`github.com/signintech/gopdf`) | `v0.38.1` | Формирование PDF-документов и договоров. |
| `golang.org/x/image` | `v0.46.0` | Встроенные Go-шрифты, используемые при формировании PDF. |
| `golang.org/x/crypto` | `v0.58.0` | Хеширование паролей с помощью bcrypt. |
| `golang.org/x/sync` | `v0.24.0` | Параллельная работа с группами задач и обработка ошибок (`errgroup`). |
| [caarlos0/env](https://github.com/caarlos0/env) (`github.com/caarlos0/env/v11`) | `v11.4.1` | Чтение конфигурации из переменных окружения. |
| [godotenv](https://github.com/joho/godotenv) (`github.com/joho/godotenv`) | `v1.5.1` | Загрузка переменных из `.env` при локальном запуске. |
| [google/uuid](https://github.com/google/uuid) (`github.com/google/uuid`) | `v1.6.0` | Создание и разбор UUID. |

## Внешние инфраструктурные сервисы

Бэкенд использует следующие сервисы; клиентские библиотеки для них перечислены выше:

- **PostgreSQL** — основная база данных; миграции выполняются Goose.
- **Redis** — клиент для Redis реализован на `go-redis`.
- **Kafka** — обмен событиями реализован на `franz-go`.
- **S3-совместимое объектное хранилище** — работа через AWS SDK for Go v2. В локальном Docker-стеке это может быть MinIO.

## Go-модули мок-сервисов

В репозитории также есть отдельные Go-модули `registry-mock`, `bki-mock`, `financials-mock` и `esign-mock`. Их `go.mod` не содержит внешних зависимостей (`require`); сторонние Go-библиотеки в этих модулях не объявлены.

## Источник и область списка

Основной источник списка — [backend/go.mod](../backend/go.mod). В таблице приведены прямые зависимости, включая зависимости AWS SDK и Kafka, объявленные отдельными модулями. Транзитивные зависимости из `require` с пометкой `indirect` намеренно не раскрыты: они подтягиваются перечисленными библиотеками.
