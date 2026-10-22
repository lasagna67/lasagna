# Демо-компании для frontend

В локальной Docker-БД подготовлены 12 пользователей роли `client`, каждый со своей подключённой компанией. Регистрация и электронная подпись уже выполнены через API. Для входа в сценарий достаточно login; повторять подключение компании не нужно.

Общий тестовый пароль: `DemoCabinet2026!`.

## Запросы

Выбрать email из таблицы ниже и выполнить:

```http
POST http://localhost:8080/api/v0/auth/login
Content-Type: application/json

{"email":"demo.ooo.good@lasagna.test","password":"DemoCabinet2026!"}
```

Из ответа взять `access_token`. Для всех следующих GET:

```http
Authorization: Bearer <access_token>
```

- `GET http://localhost:8080/api/v0/auth/me` — ID и роль пользователя.
- `GET http://localhost:8080/api/v0/companies` — компании вошедшего пользователя и их актуальные ID.
- `GET http://localhost:8080/api/v0/companies/{company_id}` — сохранённая карточка компании и полномочия.
- `GET http://localhost:8080/api/v0/companies/{company_id}/overview` — актуальная сводка реестра, БКИ и финансового мока.

Каждую компанию запрашивать с токеном соответствующего пользователя, не администратора. Токены в памятке не сохраняются; seed-сессии закрыты через logout.

## Как читать overview

- `registry.data.business_status`: `active` или `terminated` у выбранных профилей.
- `credit_history.status = ok` означает, что БКИ ответило. Отсутствие кредитной истории определяется по `credit_history.data.report.has_credit_history = false`, а не по статусу источника.
- `financials.status = not_found`, `financials.data = null` — у профиля нет финансовых данных (исходный финансовый мок возвращает `no_data`).
- `unavailable` означает ошибку источника и не равнозначен отсутствию данных.
- Суммы БКИ и финансов передаются в рублях. Средний свободный поток ниже — среднее `monthly_data[].free_cash`; текущий платёж — сумма `monthly_payment` по активным кредитам. Это описание демо-профиля, не результат скоринга и не гарантия одобрения нового кредита.
- Прекратившиеся компании подключены для демонстрации состояния `terminated`. Текущий API подключения допускает такой демо-сценарий; это не подтверждает право выдачи им кредита.

## Аккаунты и ID

| Ключ | Компания | ИНН | Email | Company ID |
|---|---|---|---|---|
| ooo-good | ООО «Кедровый контур Демо» | 7705005410 | demo.ooo.good@lasagna.test | 926eebcb-d04f-46a7-ab75-d2323b0502dd |
| ooo-thin | ООО «Ладожский хлеб Демо» | 7810969607 | demo.ooo.thin@lasagna.test | b2b0cc17-3641-4c38-8e34-16b3796cc561 |
| ooo-overdue | ООО «Южный каркас Демо» | 2310569351 | demo.ooo.overdue@lasagna.test | 74749f5d-ebff-4a96-a1d6-a5ecd374598a |
| ooo-no-history | ООО «Листок данных Демо» | 1655501828 | demo.ooo.no-history@lasagna.test | ed750a38-78f9-48ef-b9da-47e60499beed |
| ooo-no-data | ООО «Искра макетов Демо» | 6671986865 | demo.ooo.no-data@lasagna.test | 16ed6bac-0026-4fe0-bde2-ccbb874d042d |
| ooo-terminated | ООО «Волжский ассортимент Демо» | 5260947681 | demo.ooo.terminated@lasagna.test | 0efdcd1d-a7db-41d8-bc51-03bfd1b0ed2d |
| ip-good | ИП Кедровцев Матвей Олегович | 770761836401 | demo.ip.good@lasagna.test | 6210e8b0-0929-44ba-82a4-b1fa93b50969 |
| ip-thin | ИП Ладогина Вероника Ильинична | 781497718575 | demo.ip.thin@lasagna.test | 1dd7ac0d-f03d-4491-b36d-de1234d9a1c3 |
| ip-overdue | ИП Листопадова Марина Денисовна | 770815348700 | demo.ip.overdue@lasagna.test | ada83d1c-014d-48d0-9a05-8986f06cbc15 |
| ip-no-history | ИП Яснолесов Даниил Романович | 165979491871 | demo.ip.no-history@lasagna.test | 231ee67d-a895-4f81-a0bc-d5b303d39482 |
| ip-no-data | ИП Берестина Алиса Тимуровна | 667036260874 | demo.ip.no-data@lasagna.test | 2f85cffa-4f64-4e6a-ad29-26ea4c3597ec |
| ip-terminated | ИП Ручейников Глеб Павлович | 526200583660 | demo.ip.terminated@lasagna.test | 2fce9a9e-6990-40d3-86c0-a31de2701cc3 |

## Готовые запросы сводки

| Ключ | Сценарий | Запрос |
|---|---|---|
| ooo-good | Хорошая кредитная история и достаточные финансы | GET http://localhost:8080/api/v0/companies/926eebcb-d04f-46a7-ab75-d2323b0502dd/overview |
| ooo-thin | Небольшой запас денег | GET http://localhost:8080/api/v0/companies/b2b0cc17-3641-4c38-8e34-16b3796cc561/overview |
| ooo-overdue | Текущая просрочка | GET http://localhost:8080/api/v0/companies/74749f5d-ebff-4a96-a1d6-a5ecd374598a/overview |
| ooo-no-history | Нет кредитной истории, есть финансовые данные | GET http://localhost:8080/api/v0/companies/ed750a38-78f9-48ef-b9da-47e60499beed/overview |
| ooo-no-data | Нет кредитной истории и финансовых данных | GET http://localhost:8080/api/v0/companies/16ed6bac-0026-4fe0-bde2-ccbb874d042d/overview |
| ooo-terminated | Деятельность прекращена | GET http://localhost:8080/api/v0/companies/0efdcd1d-a7db-41d8-bc51-03bfd1b0ed2d/overview |
| ip-good | Хорошая кредитная история и достаточные финансы | GET http://localhost:8080/api/v0/companies/6210e8b0-0929-44ba-82a4-b1fa93b50969/overview |
| ip-thin | Небольшой запас денег | GET http://localhost:8080/api/v0/companies/1dd7ac0d-f03d-4491-b36d-de1234d9a1c3/overview |
| ip-overdue | Текущая просрочка | GET http://localhost:8080/api/v0/companies/ada83d1c-014d-48d0-9a05-8986f06cbc15/overview |
| ip-no-history | Нет кредитной истории, есть финансовые данные | GET http://localhost:8080/api/v0/companies/231ee67d-a895-4f81-a0bc-d5b303d39482/overview |
| ip-no-data | Нет кредитной истории и финансовых данных | GET http://localhost:8080/api/v0/companies/2f85cffa-4f64-4e6a-ad29-26ea4c3597ec/overview |
| ip-terminated | Деятельность прекращена | GET http://localhost:8080/api/v0/companies/2fce9a9e-6990-40d3-86c0-a31de2701cc3/overview |

## Проверенные показатели

| Ключ | Реестр | Есть КИ | Финансы | Средний свободный поток, руб./мес. | Текущий платёж, руб./мес. | Текущая просрочка, руб. |
|---|---|---|---|---|---|---|
| ooo-good | active | True | ok | 2011500 | 450000 | 0 |
| ooo-thin | active | True | ok | 98500 | 95000 | 0 |
| ooo-overdue | active | True | ok | 201250 | 350000 | 700000 |
| ooo-no-history | active | False | ok | 94500 | 0 | 0 |
| ooo-no-data | active | False | not_found |  | 0 | 0 |
| ooo-terminated | terminated | True | not_found |  | 0 | 0 |
| ip-good | active | True | ok | 201150 | 45000 | 0 |
| ip-thin | active | True | ok | 38500 | 35000 | 0 |
| ip-overdue | active | True | ok | 43125 | 75000 | 150000 |
| ip-no-history | active | False | ok | 65880 | 0 | 0 |
| ip-no-data | active | False | not_found |  | 0 | 0 |
| ip-terminated | terminated | True | not_found |  | 0 | 0 |

## Передача другому разработчику

`localhost` в URL — компьютер, на котором запущен backend. Для общего стенда заменить адрес API. Аккаунты созданы в текущей локальной БД; на другой БД сначала выполнить seed, затем брать ID из нового JSON или GET /companies. UUID при пересоздании БД изменятся.

Машиночитаемые аккаунты и URL: `docs/demo-businesses.json`.

Повторное создание на поднятом окружении (из корня репозитория lasagna):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\seed-demo-businesses.ps1
```

Для другого окружения скрипт принимает BaseUrl, RegistryUrl, BkiUrl, FinancialsUrl, ESignUrl. Использовать URL моков соответствующего backend. Повторный запуск переиспользует аккаунты и компании, проверяет сводки и обновляет памятку. Он не сбрасывает БД и не меняет существующие пароли. Конфликт email с другим паролем завершит запуск ошибкой.