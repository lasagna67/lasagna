import type { Overview, SourceStatus } from "@/lib/api";
import {
  businessStatusLabel,
  dateRu,
  loanTypeLabel,
  loanStatusLabel,
  money,
} from "@/lib/demo-data";

const badges: Record<SourceStatus, { label: string; cls: string }> = {
  ok: { label: "Данные получены", cls: "bg-plate-menthol" },
  not_found: { label: "Нет данных", cls: "bg-plate-chardonnay" },
  unavailable: { label: "Источник недоступен", cls: "bg-plate-tomato" },
};

const sourceNotes: Record<string, string> = {
  registry_not_found: "Компания не найдена в реестре",
  registry_unavailable: "Реестр временно недоступен",
  bki_not_found: "Отчёт по кредитной истории не найден",
  bki_unavailable: "БКИ временно недоступно",
  fin_not_found: "Финансовых данных нет",
  fin_unavailable: "Источник временно недоступен",
};

function SourceCard({
  title,
  status,
  children,
}: {
  title: string;
  status: SourceStatus;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-card-lg border border-black/8 p-6">
      <div className="flex items-center justify-between gap-4">
        <h3 className="text-[18px] font-medium">{title}</h3>
        <span className={`shrink-0 rounded-full px-3 py-1 text-[13px] ${badges[status].cls}`}>
          {badges[status].label}
        </span>
      </div>
      <div className="mt-4 text-[15px] leading-6">{children}</div>
    </section>
  );
}

function Note({ children }: { children: React.ReactNode }) {
  return <p className="text-ink-soft">{children}</p>;
}

export function OverviewSection({ overview }: { overview: Overview }) {
  const { registry, credit_history: ch, financials } = overview;
  const report = ch.data?.report;

  return (
    <div className="mt-6 grid gap-6 md:grid-cols-2">
      <SourceCard title="Реестр (ЕГРЮЛ / ЕГРИП)" status={registry.status}>
        {registry.data ? (
          <>
            <p className="font-medium">{registry.data.name}</p>
            <p className="mt-1 text-ink-soft">
              ИНН {registry.data.inn} · {businessStatusLabel(registry.data.business_status)} ·
              зарегистрирована {dateRu(registry.data.registration_date)}
            </p>
            {registry.data.head && (
              <p className="mt-2">
                Руководитель: {registry.data.head.full_name} ({registry.data.head.position})
              </p>
            )}
            {registry.data.okved && registry.data.okved.length > 0 && (
              <p className="mt-2 text-ink-soft">
                ОКВЭД: {registry.data.okved.slice(0, 3).map((a) => a.code).join(", ")}
                {registry.data.okved.length > 3 && ` и ещё ${registry.data.okved.length - 3}`}
              </p>
            )}
          </>
        ) : (
          <Note>
            {registry.status === "unavailable"
              ? sourceNotes.registry_unavailable
              : sourceNotes.registry_not_found}
          </Note>
        )}
      </SourceCard>

      <SourceCard title="Кредитная история (БКИ)" status={ch.status}>
        {ch.status !== "ok" || !report ? (
          <Note>
            {ch.status === "unavailable"
              ? sourceNotes.bki_unavailable
              : sourceNotes.bki_not_found}
          </Note>
        ) : !report.has_credit_history ? (
          <Note>БКИ ответило, кредитной истории нет.</Note>
        ) : (
          <>
            <p className="text-ink-soft">
              История: {report.history_months} мес. · кредитов: {report.loans.length}
            </p>
            <table className="mt-3 w-full text-[14px]">
              <thead className="text-left text-ink-soft">
                <tr>
                  <th className="py-1 font-normal">Кредит</th>
                  <th className="py-1 font-normal">Статус</th>
                  <th className="py-1 font-normal">Остаток</th>
                  <th className="py-1 font-normal">Просрочка</th>
                </tr>
              </thead>
              <tbody>
                {report.loans.map((loan, i) => (
                  <tr key={i} className="border-t border-black/6">
                    <td className="py-2 pr-2">{loanTypeLabel(loan.loan_type)}</td>
                    <td className="py-2 pr-2">{loanStatusLabel(loan.status)}</td>
                    <td className="py-2 pr-2">{money(loan.outstanding_amount)}</td>
                    <td className={`py-2 ${loan.overdue_amount > 0 ? "text-brand" : ""}`}>
                      {loan.overdue_amount > 0
                        ? `${money(loan.overdue_amount)} (${loan.overdue_days} дн.)`
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </SourceCard>

      <div className="md:col-span-2">
        <SourceCard title="Финансовые данные" status={financials.status}>
          {financials.status !== "ok" || !financials.data ? (
            <Note>
              {financials.status === "unavailable"
                ? sourceNotes.fin_unavailable
                : sourceNotes.fin_not_found}
            </Note>
          ) : (
            <>
              <p className="text-ink-soft">
                Период: {dateRu(financials.data.period?.from ?? "")} —{" "}
                {dateRu(financials.data.period?.to ?? "")} · средний свободный поток:{" "}
                {money(
                  Math.round(
                    financials.data.monthly_data.reduce((sum, m) => sum + m.free_cash, 0) /
                      Math.max(1, financials.data.monthly_data.length),
                  ),
                )}
                /мес.
              </p>
              <table className="mt-3 w-full text-[14px]">
                <thead className="text-left text-ink-soft">
                  <tr>
                    <th className="py-1 font-normal">Месяц</th>
                    <th className="py-1 font-normal">Доход</th>
                    <th className="py-1 font-normal">Расход</th>
                    <th className="py-1 font-normal">Свободный поток</th>
                  </tr>
                </thead>
                <tbody>
                  {[...financials.data.monthly_data]
                    .slice(-6)
                    .reverse()
                    .map((month) => (
                      <tr key={month.month} className="border-t border-black/6">
                        <td className="py-2 pr-2">{month.month}</td>
                        <td className="py-2 pr-2">{money(month.income)}</td>
                        <td className="py-2 pr-2">{money(month.expenses)}</td>
                        <td className="py-2">{money(month.free_cash)}</td>
                      </tr>
                    ))}
                </tbody>
              </table>
            </>
          )}
        </SourceCard>
      </div>
    </div>
  );
}
