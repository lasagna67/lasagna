# Financial Mock

Самостоятельный мок финансов на Go 1.26 и стандартной библиотеке. Реализован по образцу BKI Mock: Handler → Service → Repository, зависимости через конструкторы, `net/http` и общий JSON-логгер `slog`. 


## API

`GET /v1/financial-reports/{inn}` получает финансовый отчёт по ИНН из 10 или 12 ASCII-цифр. Тело запроса не требуется. Контрольные цифры ИНН не проверяются.

```powershell
Invoke-RestMethod -Method Get -Uri 'http://localhost:8083/v1/financial-reports/7705005410'
```

Ответ `200` содержит исходные `business_id`, `inn`, `status`, `currency`, `period`, `monthly_data`. Все денежные поля хранятся в `int64`, значения берутся из JSON без пересчёта.

Если у известного бизнеса нет финансовых данных, возвращается HTTP `200` с исходным объектом:

```json
{
  "business_id": "business_004",
  "inn": "6671986865",
  "status": "no_data",
  "currency": "RUB",
  "period": null,
  "monthly_data": null,
  "error_code": "FINANCIAL_DATA_NOT_FOUND",
  "message": "Финансовые данные отсутствуют"
}
```

Отсутствие самого ИНН в файле возвращает HTTP `404`:

```json
{"error":{"code":"FINANCIAL_DATA_NOT_FOUND","message":"Financial data not found"}}
```

Остальные ошибки: `400 INVALID_INN`, `404 NOT_FOUND` для неизвестного маршрута, `405 METHOD_NOT_ALLOWED` с `Allow: GET`, `500 INTERNAL_ERROR`. Техническая ручка `GET /health` возвращает `{"status":"ok"}`.

Сервис использует только `testdata/financials.json`; вызовов БКИ, реестров и других сервисов нет. В исходном файле 20 синтетических профилей: 14 с финансовыми данными и 6 со статусом `no_data`.


