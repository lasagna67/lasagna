"use client";

import Link from "next/link";
import { appStatusLabel, kopecks, purposeLabel } from "@/lib/demo-data";
import type { Application } from "@/lib/types-application";
import { Button } from "@/components/ui/button";

type Props = {
  items: Application[];
  busy: boolean;
  canCreate: boolean;
  onCreate: () => void;
};

const statusPlates: Record<string, string> = {
  draft: "bg-plate-pearl",
  approved: "bg-plate-menthol",
  counter_offer: "bg-plate-lagoon",
  rejected: "bg-plate-tomato",
  info_requested: "bg-plate-chardonnay",
};

export function ApplicationsSection({ items, busy, canCreate, onCreate }: Props) {
  return (
    <section className="mt-10" data-tour="applications">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h2 className="text-promo-s">Заявки</h2>
        <Button size="m" disabled={!canCreate || busy} onClick={onCreate}>
          {busy ? "Создаём…" : "Новая заявка"}
        </Button>
      </div>

      {items.length === 0 ? (
        <p className="mt-4 text-[15px] text-ink-soft">
          Заявок пока нет — создайте первую, чтобы пройти путь от параметров кредита до подписания
          договора.
        </p>
      ) : (
        <ul className="mt-4 divide-y divide-black/6 rounded-card-lg border border-black/6">
          {items.map((app) => (
            <li key={app.id}>
              <Link
                href={`/cabinet/applications/${app.id}`}
                className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-4 transition-colors hover:bg-black/3"
              >
                <span className="font-mono text-[14px] text-ink-soft">№{app.number}</span>
                <span className="text-[16px] font-medium">
                  {purposeLabel(app.request.purpose)}
                  {app.request.amount_kopecks != null && ` · ${kopecks(app.request.amount_kopecks)}`}
                </span>
                <span
                  className={`ml-auto rounded-full px-3 py-1 text-[13px] ${statusPlates[app.status] ?? "bg-plate-sonata"}`}
                >
                  {appStatusLabel(app.status)}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
