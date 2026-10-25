import { dateRu, kopecks, riskGradeLabel } from "@/lib/demo-data";
import type { Checks, CashPlanAssessment, Scoring } from "@/lib/types-underwriter";
import { AppSummary } from "@/components/application/app-summary";
import type { ReviewCard } from "@/lib/types-underwriter";

type Props = { card: ReviewCard };

const checkLabels: Record<string, string> = {
  ok: "ОК",
  not_requested: "не запрошены",
  not_found: "не найден",
  unavailable: "недоступны",
  invalid: "некорректны",
};

const scoreLabels: Record<string, string> = {
  completed: "завершён",
  insufficient_data: "недостаточно данных",
  not_configured: "не настроен",
  failed: "ошибка расчёта",
  skipped: "пропущен",
};

const recLabels: Record<string, string> = {
  manual_review: "на ручную проверку",
  auto_approved: "авто-одобрение",
  auto_rejected: "авто-отклонение",
};

function Chip({ text, ok }: { text: string; ok?: boolean }) {
  return (
    <span className={`rounded-full px-3 py-1 text-[13px] ${ok === false ? "bg-plate-tomato" : "bg-plate-sonata"}`}>
      {text}
    </span>
  );
}

function ChecksBlock({ checks }: { checks: Checks }) {
  return (
    <section className="rounded-card-lg border border-black/8 p-5">
      <h3 className="text-[16px] font-medium">Проверки</h3>
      <div className="mt-3 grid gap-3 md:grid-cols-3">
        <div className="rounded-2xl bg-plate-pearl/70 px-4 py-3 text-[14px]">
          <p className="font-medium">Реестр</p>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            <Chip text={checkLabels[checks.registry.status] ?? checks.registry.status} ok={checks.registry.status === "ok"} />
            {checks.registry.checked_at && <span className="text-ink-soft">{dateRu(checks.registry.checked_at)}</span>}
          </div>
          {checks.registry.inn && <p className="mt-1 text-ink-soft">ИНН {checks.registry.inn}</p>}
          {checks.registry.company_state && <p className="text-ink-soft">состояние: {checks.registry.company_state}</p>}
        </div>
        <div className="rounded-2xl bg-plate-pearl/70 px-4 py-3 text-[14px]">
          <p className="font-medium">БКИ</p>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            <Chip text={checkLabels[checks.credit_bureau.status] ?? checks.credit_bureau.status} ok={checks.credit_bureau.status === "ok"} />
            {checks.credit_bureau.checked_at && <span className="text-ink-soft">{dateRu(checks.credit_bureau.checked_at)}</span>}
          </div>
          {checks.credit_bureau.has_credit_history != null && (
            <p className="mt-1 text-ink-soft">
              кредитная история: {checks.credit_bureau.has_credit_history ? "есть" : "нет"}
            </p>
          )}
        </div>
        <div className="rounded-2xl bg-plate-pearl/70 px-4 py-3 text-[14px]">
          <p className="font-medium">Финансы</p>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            <Chip
              text={
                checks.financials.data_status === "ready"
                  ? "получены"
                  : checks.financials.data_status === "no_history"
                    ? "нет истории"
                    : "не получены"
              }
              ok={checks.financials.data_status !== "missing"}
            />
            {checks.financials.source_type && <span className="text-ink-soft">{checks.financials.source_type}</span>}
            {checks.financials.coverage && <span className="text-ink-soft">покрытие: {checks.financials.coverage}</span>}
          </div>
        </div>
      </div>
    </section>
  );
}

function ScoringBlock({ scoring }: { scoring: Scoring }) {
  return (
    <section className="rounded-card-lg border border-black/8 p-5">
      <h3 className="text-[16px] font-medium">Скоринг</h3>
      <div className="mt-3 flex flex-wrap items-center gap-3 text-[14px]">
        <Chip text={scoreLabels[scoring.status] ?? scoring.status} ok={scoring.status === "completed"} />
        {scoring.kind && <span className="rounded-full bg-plate-lagoon px-3 py-1 text-[13px]">{scoring.kind}</span>}
        {scoring.score != null && <span className="font-medium">балл {scoring.score}</span>}
        {scoring.risk_grade && <span>риск: {riskGradeLabel(scoring.risk_grade)}</span>}
        {scoring.recommendation && <span className="text-ink-soft">маршрут: {recLabels[scoring.recommendation]}</span>}
        <span className={`rounded-full px-3 py-1 text-[13px] ${scoring.can_publish ? "bg-plate-menthol" : "bg-plate-pearl"}`}>
          {scoring.can_publish ? "можно публиковать решение" : "публикация ограничена"}
        </span>
        {scoring.calculated_at && <span className="text-ink-soft">{dateRu(scoring.calculated_at)}</span>}
        {scoring.model_version && <span className="text-ink-soft">модель {scoring.model_version}</span>}
      </div>
      {scoring.positive_blockers.length > 0 && (
        <ul className="mt-3 list-disc pl-5 text-[14px] text-brand">
          {scoring.positive_blockers.map((b, i) => (
            <li key={i}>{b}</li>
          ))}
        </ul>
      )}
    </section>
  );
}

function AssessmentBlock({ a }: { a: CashPlanAssessment }) {
  if (!a.required && a.signals.length === 0 && !a.terms) return null;
  return (
    <section className="rounded-card-lg border border-black/8 p-5">
      <h3 className="text-[16px] font-medium">Денежный план: оценка</h3>
      <div className="mt-3 flex flex-wrap items-center gap-3 text-[14px]">
        <Chip
          text={a.required == null ? "нужность не определена" : a.required ? "план требуется" : "план не требуется"}
        />
        <span className={`rounded-full px-3 py-1 text-[13px] ${a.review_current ? "bg-plate-menthol" : "bg-plate-chardonnay"}`}>
          заключение {a.review_current ? "актуально" : "устарело"}
        </span>
        {a.terms && (
          <span className="text-ink-soft">
            проверка на условиях: {a.terms.product_code},{" "}
            {kopecks(a.terms.amount_kopecks)}, {a.terms.term_months ?? "—"} мес.
          </span>
        )}
      </div>
      {a.signals.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-2">
          {a.signals.map((s) => (
            <span key={s} className="rounded-full bg-plate-pearl px-3 py-1 text-[13px]">
              {s}
            </span>
          ))}
        </div>
      )}
      {a.blockers.length > 0 && (
        <ul className="mt-3 list-disc pl-5 text-[14px] text-brand">
          {a.blockers.map((b, i) => (
            <li key={i}>{b.message}</li>
          ))}
        </ul>
      )}
    </section>
  );
}

export function ReviewSections({ card }: Props) {
  return (
    <div className="mt-6 space-y-6">
      <AppSummary app={card} />
      <ChecksBlock checks={card.checks} />
      <ScoringBlock scoring={card.scoring} />
      <AssessmentBlock a={card.cash_plan_assessment} />
    </div>
  );
}
