import { dateRu, kopecks } from "@/lib/demo-data";
import type { Application } from "@/lib/types-application";

type Props = { app: Application };

const resultLabels: Record<string, string> = {
  accepted: "Принято",
  needs_info: "Нужны уточнения",
  rejected: "Отклонено",
};

const resultPlates: Record<string, string> = {
  accepted: "bg-plate-menthol",
  needs_info: "bg-plate-chardonnay",
  rejected: "bg-plate-tomato",
};

export function ResultChip({ result }: { result: string }) {
  return (
    <span className={`rounded-full px-3 py-1 text-[13px] ${resultPlates[result] ?? "bg-plate-pearl"}`}>
      {resultLabels[result] ?? result}
    </span>
  );
}

export function BankReviews({ app }: Props) {
  const plan = app.plan_review;
  const coll = app.collateral?.review;
  if (!plan && !coll) return null;
  return (
    <section className="mt-6 rounded-card-lg border border-black/8 p-5">
      <h3 className="text-[16px] font-medium">Комментарии банка</h3>
      <div className="mt-3 space-y-3 text-[15px]">
        {plan && (
          <div className="rounded-2xl bg-plate-pearl/70 px-4 py-3">
            <div className="flex flex-wrap items-center gap-3">
              <span className="font-medium">Денежный план</span>
              <ResultChip result={plan.result} />
              <span className="text-[13px] text-ink-soft">{dateRu(plan.reviewed_at)}</span>
            </div>
            <p className="mt-1 text-ink-soft">
              Проверенный резерв:{" "}
              {plan.validated_reserve_kopecks != null
                ? kopecks(plan.validated_reserve_kopecks)
                : "—"}
            </p>
            {plan.public_comment && <p className="mt-1">{plan.public_comment}</p>}
          </div>
        )}
        {coll && (
          <div className="rounded-2xl bg-plate-pearl/70 px-4 py-3">
            <div className="flex flex-wrap items-center gap-3">
              <span className="font-medium">Залог</span>
              <ResultChip result={coll.result} />
              <span className="text-[13px] text-ink-soft">{dateRu(coll.reviewed_at)}</span>
            </div>
            <p className="mt-1 text-ink-soft">
              Принятая стоимость:{" "}
              {coll.accepted_value_kopecks != null ? kopecks(coll.accepted_value_kopecks) : "—"}
            </p>
            {coll.public_comment && <p className="mt-1">{coll.public_comment}</p>}
          </div>
        )}
      </div>
    </section>
  );
}
