# Lasagna

## Запуск демо

Нужен Docker с Compose. Перед первым запуском скопировать `.env.example` в `.env.docker` и проверить параметры окружения.

Из каталога `backend`:

```sh
docker compose up -d --build
```

Compose поднимет базы и моки, применит миграции и запустит backend. Одноразовый сервис `demo-seed` дождётся готовности API и создаст 12 демо-аккаунтов с подключёнными компаниями: по шесть сценариев для ООО и ИП. Повторный запуск переиспользует существующие записи.

Дождаться окончания заполнения:

```sh
docker compose wait demo-seed
docker compose logs demo-seed
```

Код завершения `0` и сообщение `demo seed completed accounts=12` означают готовность демо. Backend доступен на `http://localhost:8080`.

Логины и API для frontend: [docs/demo-businesses.md](docs/demo-businesses.md). Общий демо-пароль: `DemoCabinet2026!`. После входа актуальные ID компаний получать через `GET /api/v0/companies`: на новой БД UUID отличаются.

Повторить заполнение вручную:

```sh
docker compose run --rm --no-deps demo-seed
```

Актуальный список аккаунтов сохраняется в volume `demo-data`. Из каталога `backend` его можно выгрузить:

```sh
docker compose cp demo-seed:/data/demo-businesses.json ../docs/demo-businesses.json
```
