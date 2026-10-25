"use client";

import { useState } from "react";
import { api } from "@/lib/api";
import { kopecks } from "@/lib/demo-data";
import type { Application } from "@/lib/types-application";
import { FieldErrors, errorFields, errorMessage, primary, secondary } from "./form-ui";

type Props = {
  app: Application;
  token: string;
  error: unknown;
  onDone: () => Promise<boolean>;
};

const sourcePlates: Record<string, string> = {
  ok: "bg-plate-menthol",
  not_found: "bg-plate-chardonnay",
  unavailable: "bg-plate-tomato",
  invalid: "bg-plate-tomato",
  not_requested: "bg-plate-pearl",
};

export function FinancialsPanel({ app, token, error, onDone }: Props) {
  const fin = app.financials;
  const [busy, setBusy] = useState<"" | "fetch" | "csv" | "no-history">("");
  const [localError, setLocalError] = useState("");
  const actions = new Set(app.available_actions);

  async function run(kind: typeof busy, fn: () => Promise<unknown>) {
    setBusy(kind);
    setLocalError("");
    try {
      await fn();
      await onDone();
    } catch (e) {
      setLocalError(errorMessage(e));
      await onDone();
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3 text-[14px]">
        <span className={`rounded-full px-3 py-1 ${sourcePlates[fin.source_status] ?? "bg-plate-pearl"}`}>
          Источник: {fin.source_status}
        </span>
        <span className="rounded-full bg-plate-sonata px-3 py-1">
          Данные:{" "}
          {fin.data_status === "ready"
            ? "получены"
            : fin.data_status === "no_history"
              ? "нет истории"
              : "не получены"}
        </span>
        {fin.manual_required && (
          <span className="rounded-full bg-plate-chardonnay px-3 py-1">Понадобится проверка сотрудника</span>
        )}
        {fin.snapshot && (
          <span className="text-ink-soft">
            {fin.snapshot.period_from} — {fin.snapshot.period_to} · {fin.snapshot.provenance.source_type}
            {fin.snapshot.provenance.file_name ? ` · ${fin.snapshot.provenance.file_name}` : ""}
          </span>
        )}
      </div>

      {fin.snapshot && (
        <div className="max-h-[240px] overflow-y-auto rounded-2xl border border-black/8 text-[14px]">
          <table className="w-full">
            <thead className="sticky top-0 bg-plate-pearl text-left text-ink-soft">
              <tr>
                <th className="px-3 py-2 font-normal">Месяц</th>
                <th className="px-3 py-2 text-right font-normal">Поступления</th>
                <th className="px-3 py-2 text-right font-normal">Выплаты</th>
                <th className="px-3 py-2 text-right font-normal">Налоги</th>
                <th className="px-3 py-2 text-right font-normal">Владелец</th>
                <th className="px-3 py-2 text-right font-normal">Своб. средства</th>
              </tr>
            </thead>
            <tbody>
              {fin.snapshot.rows.map((row) => (
                <tr key={row.month} className="border-t border-black/6">
                  <td className="px-3 py-1.5 font-mono text-[13px]">{row.month}</td>
                  <td className="px-3 py-1.5 text-right">{kopecks(row.income_kopecks)}</td>
                  <td className="px-3 py-1.5 text-right">{kopecks(row.expenses_kopecks)}</td>
                  <td className="px-3 py-1.5 text-right">{kopecks(row.taxes_kopecks)}</td>
                  <td className="px-3 py-1.5 text-right">{kopecks(row.owner_withdrawals_kopecks)}</td>
                  <td className="px-3 py-1.5 text-right font-medium">{kopecks(row.free_cash_kopecks)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3">
        {actions.has("fetch_financials") && (
          <button
            type="button"
            className={primary}
            disabled={busy !== ""}
            onClick={() => void run("fetch", () => api.fetchFinancials(app.id, { expected_revision: app.revision }, token))}
          >
            {busy === "fetch" ? "Запрашиваем…" : "Получить данные из источника"}
          </button>
        )}
        {fin.allow_csv_upload && (
          <label className={`${secondary} cursor-pointer ${busy === "csv" ? "opacity-60" : ""}`}>
            {busy === "csv" ? "Загружаем…" : "Загрузить CSV"}
            <input
              type="file"
              accept=".csv,text/csv"
              className="sr-only"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) void run("csv", () => api.uploadAppCsv(app.id, file, app.revision, token));
                e.target.value = "";
              }}
            />
          </label>
        )}
        {fin.allow_no_history && actions.has("declare_no_history") && (
          <button
            type="button"
            className={secondary}
            disabled={busy !== ""}
            onClick={() =>
              void run("no-history", () =>
                api.declareNoHistory(app.id, { expected_revision: app.revision, confirmed: true }, token),
              )
            }
          >
            Истории нет — подтверждаю
          </button>
        )}
      </div>

      {fin.issues.length > 0 && (
        <ul className="rounded-2xl bg-plate-chardonnay/60 px-4 py-3 text-[14px] leading-5">
          {fin.issues.map((issue, i) => (
            <li key={i}>{issue.message}</li>
          ))}
        </ul>
      )}

      {localError && <p className="text-[14px] text-brand">{localError}</p>}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}
    </div>
  );
}
