"use client";

import { appStatusLabel, dateRu, pipelineStages } from "@/lib/demo-data";
import type { AppStatus } from "@/lib/types-application";

type Props = { status: AppStatus; number: number; submittedAt: string | null };

const asyncStages = new Set<AppStatus>(["submitted", "validation", "enrichment", "scoring"]);

const plates: Record<string, string> = {
  draft: "bg-plate-pearl",
  approved: "bg-plate-menthol",
  counter_offer: "bg-plate-lagoon",
  rejected: "bg-plate-tomato",
  info_requested: "bg-plate-chardonnay",
  signing: "bg-plate-lagoon",
  issued: "bg-plate-menthol",
};

export function StatusBar({ status, number, submittedAt }: Props) {
  const stageIdx = pipelineStages.indexOf(status as (typeof pipelineStages)[number]);
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
      <h1 className="text-promo-m">Заявка №{number}</h1>
      <span className={`rounded-full px-3 py-1 text-[14px] ${plates[status] ?? "bg-plate-sonata"}`}>
        {appStatusLabel(status)}
      </span>
      {submittedAt && (
        <span className="text-[14px] text-ink-soft">от {dateRu(submittedAt)}</span>
      )}
      {asyncStages.has(status) && (
        <ol className="flex flex-wrap gap-2 text-[13px] text-ink-soft">
          {pipelineStages.map((stage, i) => (
            <li
              key={stage}
              className={`rounded-full px-3 py-1 ${i < stageIdx ? "bg-plate-menthol text-ink" : i === stageIdx ? "bg-plate-sonata font-medium text-ink" : "bg-plate-pearl"}`}
            >
              {i < stageIdx ? "✓ " : ""}
              {appStatusLabel(stage)}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
