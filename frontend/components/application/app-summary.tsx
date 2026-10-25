import { collateralTypeLabel, kopecks, purposeLabel } from "@/lib/demo-data";
import type { Application } from "@/lib/types-application";

type Props = { app: Application };

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between gap-4 py-1.5 text-[15px]">
      <span className="text-ink-soft">{label}</span>
      <span className="text-right">{value}</span>
    </div>
  );
}

export function AppSummary({ app }: Props) {
  const r = app.request;
  const fin = app.financials;
  const finText =
    fin.data_status === "ready"
      ? `Данные получены (${fin.snapshot?.period_from} — ${fin.snapshot?.period_to})`
      : fin.data_status === "no_history"
        ? "История отсутствует (подтверждено)"
        : "Данные не получены";

  return (
    <div className="grid gap-6 md:grid-cols-2">
      <section className="rounded-card-lg border border-black/8 p-5">
        <h3 className="text-[16px] font-medium">Параметры заявки</h3>
        <div className="mt-2">
          <Row label="Цель" value={purposeLabel(r.purpose) + (r.purpose === "other" && r.purpose_description ? ` — ${r.purpose_description}` : "")} />
          <Row label="Сумма" value={r.amount_kopecks != null ? kopecks(r.amount_kopecks) : "—"} />
          <Row
            label="Срок"
            value={
              r.term_months
                ? `${r.term_months} мес.`
                : r.need_period.unit === "days" && r.need_period.value
                  ? `${r.need_period.value} дн. (транш)`
                  : "—"
            }
          />
          <Row label="Продукт" value={r.product_code ?? "—"} />
          <Row label="Обеспечение" value={r.collateral_mode === "pledged" ? "С залогом" : "Без залога"} />
          <Row label="Финансы" value={finText} />
        </div>
      </section>

      {app.collateral && (
        <section className="rounded-card-lg border border-black/8 p-5">
          <h3 className="text-[16px] font-medium">Залог</h3>
          <div className="mt-2">
            <Row label="Предмет" value={collateralTypeLabel(app.collateral.type)} />
            <Row label="Описание" value={app.collateral.description} />
            <Row label="Заявленная стоимость" value={kopecks(app.collateral.declared_value_kopecks)} />
          </div>
        </section>
      )}

      {app.product_details && (
        <section className="rounded-card-lg border border-black/8 p-5">
          <h3 className="text-[16px] font-medium">Проект «Старт»</h3>
          <div className="mt-2">
            <Row label="Описание" value={app.product_details.project_description ?? "—"} />
            <Row label="Стоимость проекта" value={app.product_details.project_cost_kopecks != null ? kopecks(app.product_details.project_cost_kopecks) : "—"} />
            <Row label="Собственные средства" value={app.product_details.own_funds_kopecks != null ? kopecks(app.product_details.own_funds_kopecks) : "—"} />
            <Row label="Начало продаж" value={app.product_details.sales_start_month ?? "—"} />
          </div>
        </section>
      )}

      {app.cash_plan && (
        <section className="rounded-card-lg border border-black/8 p-5">
          <h3 className="text-[16px] font-medium">
            Денежный план {app.cash_plan.confirmed ? "· подтверждён" : "· не подтверждён"}
          </h3>
          <div className="mt-2">
            <Row label="Первый месяц" value={app.cash_plan.first_month} />
            <Row label="Резерв" value={kopecks(app.cash_plan.own_reserve_kopecks)} />
            <Row label="Месяцев в прогнозе" value={app.cash_plan.forecast.length} />
          </div>
        </section>
      )}
    </div>
  );
}
