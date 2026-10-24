# Загрузка финансовой отчётности

`POST /api/v0/companies/{company_id}/financials`

Доступен авторизованному клиенту, который состоит в выбранной компании.

Передать `multipart/form-data`, поле файла — **`file`**. Размер всего запроса — до 2 МиБ.
В браузере передать `FormData`; заголовок `Content-Type` с boundary браузер выставит сам.

```javascript
const form = new FormData();
form.append("file", file);

const response = await fetch(`/api/v0/companies/${companyId}/financials`, {
  method: "POST",
  headers: { Authorization: `Bearer ${token}` },
  body: form,
});
const report = await response.json();
```

CSV: UTF-8, разделитель — запятая, заголовок и порядок колонок обязательны:

```csv
inn,month,income,expenses,taxes,owner_withdrawals
7705005410,2026-09,935000000,598400000,56100000,70000000
```

**Все суммы — целые копейки**, без десятичной точки. ИНН должен совпадать с ИНН компании
во всех строках. Месяцы — `YYYY-MM`, без повторений. Суммы неотрицательные; свободные
средства могут быть отрицательными:

`free_cash = income - expenses - taxes - owner_withdrawals`

[Полный пример CSV](../backend/internal/financials-csv/parser/testdata/financials_example.csv).

Файл целиком проверяется до записи. При успехе возвращается `200` и сохранённый
`FinancialReport` с ИНН, валютой `RUB`, периодом и отсортированными `monthly_data`.
Загрузка заменяет финансовый отчёт в `company_overviews`, реестр и БКИ сохраняются.
Сохраняются данные отчёта; отдельного хранилища исходных файлов нет.

Код загрузки находится в `backend/internal/financials-csv`: `handler` принимает файл,
`service` проверяет доступ и вызывает `parser` для разбора CSV. Затем сервис вызывает
`CompanyRepo.SaveOverview` из `backend/internal/company/repo/overview.go`.
Именно этот метод выполняет `INSERT ... ON CONFLICT` в PostgreSQL: отчёт попадает
в JSONB-колонку `company_overviews.financials_data`, статус — в `financials_status`.

Существующий `GET /api/v0/companies/{company_id}/overview` использует сохранённые финансы,
если мок недоступен или в его ответе нет последнего завершённого месяца.
Для совпадающих месяцев сохраняется существующий приоритет данных мока.

Ошибки: `400` — неверный CSV/ИНН/ID или нет файла; `401` — нет авторизации;
`403` — неподходящая роль; `404` — нет доступа к компании; `413` — запрос слишком большой;
`415` — ожидается multipart; `500` — ошибка сохранения.
