import { ResultChip } from "@/components/application/bank-reviews";
import { appStatusLabel, dateRu, kopecks } from "@/lib/demo-data";
import type { ReviewCard } from "@/lib/types-underwriter";

type Props = { card: ReviewCard };

const infoStatuses: Record<string, string> = {
  open: "открыт",
  answered: "отвечен",
  cancelled: "отменён",
};

const outcomeLabels: Record<string, string> = {
  approved: "Одобрено",
  counter_offer: "Контрпредложение",
  rejected: "Отклонено",
};

function Block({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-card-lg border border-black/8 p-5">
      <h3 className="text-[16px] font-medium">{title}</h3>
      <div className="mt-3 space-y-3">{children}</div>
    </section>
  );
}

export function ReviewHistory({ card }: Props) {
  const hasReviews =
    card.info_requests.length > 0 ||
    card.collateral_reviews.length > 0 ||
    card.cash_plan_reviews.length > 0 ||
    card.decisions.length > 0 ||
    card.history.length > 0;
  if (!hasReviews) return null;

  return (
    <div className="mt-6 space-y-6">
      {card.info_requests.length > 0 && (
        <Block title="Запросы уточнений">
          {card.info_requests.map((r) => (
            <div key={r.id} className="rounded-2xl bg-plate-pearl/70 px-4 py-3 text-[14px]">
              <div className="flex flex-wrap items-center gap-3">
                <span className={`rounded-full px-3 py-1 text-[13px] ${r.status === "open" ? "bg-plate-chardonnay" : "bg-plate-pearl"}`}>
                  {infoStatuses[r.status] ?? r.status}
                </span>
                <span className="text-ink-soft">{dateRu(r.requested_at)}</span>
                {r.editable_fields.length > 0 && (
                  <span className="text-ink-soft">правка: {r.editable_fields.join(", ")}</span>
                )}
              </div>
              <p className="mt-1">{r.message}</p>
              {r.answer && <p className="mt-1 text-ink-soft">Клиент: {r.answer}</p>}
            </div>
          ))}
        </Block>
      )}

      {card.collateral_reviews.length > 0 && (
        <Block title="Проверки залога (внутренние)">
          {card.collateral_reviews.map((r) => (
            <div key={r.id} className="rounded-2xl bg-plate-pearl/70 px-4 py-3 text-[14px]">
              <div className="flex flex-wrap items-center gap-3">
                <ResultChip result={r.result} />
                {r.accepted_value_kopecks != null && (
                  <span>принято: {kopecks(r.accepted_value_kopecks)}</span>
                )}
                {!r.actual && <span className="text-brand">устарело — залог изменился</span>}
                <span className="text-ink-soft">{dateRu(r.reviewed_at)}</span>
              </div>
              <p className="mt-1">{r.comment}</p>
              {r.public_comment && <p className="text-ink-soft">Клиенту: {r.public_comment}</p>}
            </div>
          ))}
        </Block>
      )}

      {card.cash_plan_reviews.length > 0 && (
        <Block title="Заключения по денежному плану (внутренние)">
          {card.cash_plan_reviews.map((r) => (
            <div key={r.id} className="rounded-2xl bg-plate-pearl/70 px-4 py-3 text-[14px]">
              <div className="flex flex-wrap items-center gap-3">
                <ResultChip result={r.result} />
                <span>
                  резерв: {r.validated_reserve_kopecks != null ? kopecks(r.validated_reserve_kopecks) : "—"}
                </span>
                <span className="text-ink-soft">
                  условия: {r.terms.product_code} · {kopecks(r.terms.amount_kopecks)} ·{" "}
                  {r.terms.term_months ?? "—"} мес. · v{r.version}
                </span>
                {!r.actual && <span className="text-brand">устарело</span>}
                <span className="text-ink-soft">{dateRu(r.reviewed_at)}</span>
              </div>
              <p className="mt-1">{r.comment}</p>
              {r.public_comment && <p className="text-ink-soft">Клиенту: {r.public_comment}</p>}
            </div>
          ))}
        </Block>
      )}

      {card.decisions.length > 0 && (
        <Block title="Решения">
          {card.decisions.map((d) => (
            <div key={d.id} className="rounded-2xl bg-plate-pearl/70 px-4 py-3 text-[14px]">
              <div className="flex flex-wrap items-center gap-3">
                <span className="font-medium">{outcomeLabels[d.outcome] ?? d.outcome}</span>
                <span className="text-ink-soft">{dateRu(d.created_at)}</span>
                {d.reason_code && <span className="rounded-full bg-plate-sonata px-3 py-1 text-[13px]">{d.reason_code}</span>}
              </div>
              {d.offer && (
                <p className="mt-1">
                  предложение: {d.offer.product_name} · {kopecks(d.offer.amount_kopecks)} ·{" "}
                  {d.offer.term_months != null ? `${d.offer.term_months} мес.` : "овердрафт"} ·{" "}
                  {(d.offer.annual_rate_bps / 100).toLocaleString("ru-RU")}%
                </p>
              )}
              {d.reasons.length > 0 && <p className="mt-1 text-ink-soft">Причины: {d.reasons.join("; ")}</p>}
              {d.comment && <p className="text-ink-soft">Внутренний комментарий: {d.comment}</p>}
            </div>
          ))}
        </Block>
      )}

      {card.history.length > 0 && (
        <Block title="История статусов">
          <ol className="space-y-2 text-[14px]">
            {[...card.history].reverse().map((e) => (
              <li key={e.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <span className="font-mono text-[13px] text-ink-soft">{dateRu(e.created_at)}</span>
                <span>
                  {e.from_status ? `${appStatusLabel(e.from_status)} → ` : ""}
                  <span className="font-medium">{appStatusLabel(e.to_status)}</span>
                </span>
                {e.message && <span className="text-ink-soft">{e.message}</span>}
              </li>
            ))}
          </ol>
        </Block>
      )}
    </div>
  );
}
