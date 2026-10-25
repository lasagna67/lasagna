"use client";

import { useState } from "react";
import { kopecks } from "@/lib/demo-data";
import type { Application } from "@/lib/types-application";
import { FieldErrors, errorFields, errorMessage, input, label, primary, secondary } from "./form-ui";

type Props = {
  app: Application;
  error: unknown;
  onSave: (body: Record<string, unknown>) => Promise<boolean>;
};

type Row = { month: string; income: string; expenses: string };

function nextMonth(): string {
  const d = new Date();
  d.setMonth(d.getMonth() + 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

function addMonths(from: string, n: number): string {
  const [y, m] = from.split("-").map(Number);
  const d = new Date(y, m - 1 + n, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

export function CashPlanForm({ app, error, onSave }: Props) {
  const plan = app.cash_plan;
  const [firstMonth, setFirstMonth] = useState(plan?.first_month ?? nextMonth());
  const [reserve, setReserve] = useState(plan ? String(plan.own_reserve_kopecks / 100) : "");
  const [rows, setRows] = useState<Row[]>(
    plan?.forecast.map((f) => ({
      month: f.month,
      income: String(f.income_kopecks / 100),
      expenses: String(f.expenses_kopecks / 100),
    })) ?? [],
  );
  const [saved, setSaved] = useState(plan?.confirmed ?? false);
  const [localError, setLocalError] = useState("");

  const months = Math.max(app.request.term_months ?? 0, 0);
  const generated =
    rows.length === months && rows[0]?.month === firstMonth
      ? rows
      : Array.from({ length: months }, (_, i) => {
          const month = addMonths(firstMonth, i);
          const existing = rows.find((r) => r.month === month);
          return existing ?? { month, income: "", expenses: "" };
        });

  function contentBody() {
    return {
      cash_plan: {
        kind: app.product_details ? "project" : "liquidity",
        first_month: firstMonth,
        own_reserve_kopecks: Math.round((Number(reserve.replace(",", ".")) || 0) * 100),
        forecast: generated.map((r) => ({
          month: r.month,
          income_kopecks: Math.round((Number(r.income.replace(",", ".")) || 0) * 100),
          expenses_kopecks: Math.round((Number(r.expenses.replace(",", ".")) || 0) * 100),
        })),
      },
    };
  }

  async function saveContent() {
    setLocalError("");
    if (!reserve && reserve !== "0") return setLocalError("Укажите резерв (можно ноль)");
    if (generated.some((r) => r.income === "" || r.expenses === ""))
      return setLocalError("Заполните поступления и выплаты в каждом месяце");
    setSaved(await onSave(contentBody()));
  }

  async function confirm() {
    setLocalError("");
    const ok = await onSave({ cash_plan: { confirmed: true } });
    if (ok) setSaved(true);
  }

  const num = (v: string) => Number(v.replace(",", ".")) || 0;

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        {label(
          "Первый месяц прогноза",
          <input
            type="month"
            value={firstMonth}
            onChange={(e) => {
              setFirstMonth(e.target.value);
              setSaved(false);
            }}
            className={input}
          />,
        )}
        {label(
          "Резерв собственных средств, ₽",
          <input
            value={reserve}
            onChange={(e) => {
              setReserve(e.target.value);
              setSaved(false);
            }}
            inputMode="decimal"
            className={input}
          />,
        )}
      </div>

      <div className="max-h-[280px] overflow-y-auto rounded-2xl border border-black/8">
        <table className="w-full text-[14px]">
          <thead className="sticky top-0 bg-plate-pearl text-left text-ink-soft">
            <tr>
              <th className="px-3 py-2 font-normal">Месяц</th>
              <th className="px-3 py-2 text-right font-normal">Поступления, ₽</th>
              <th className="px-3 py-2 text-right font-normal">Выплаты, ₽</th>
              <th className="px-3 py-2 text-right font-normal">Своб. средства, ₽</th>
            </tr>
          </thead>
          <tbody>
            {generated.map((r, i) => (
              <tr key={r.month} className="border-t border-black/6">
                <td className="px-3 py-1.5 font-mono text-[13px]">{r.month}</td>
                <td className="px-2 py-1 text-right">
                  <input
                    value={r.income}
                    inputMode="decimal"
                    onChange={(e) => {
                      const next = [...generated];
                      next[i] = { ...r, income: e.target.value };
                      setRows(next);
                      setSaved(false);
                    }}
                    className="w-28 rounded-lg border border-black/10 px-2 py-1 text-right outline-none focus:border-brand"
                  />
                </td>
                <td className="px-2 py-1 text-right">
                  <input
                    value={r.expenses}
                    inputMode="decimal"
                    onChange={(e) => {
                      const next = [...generated];
                      next[i] = { ...r, expenses: e.target.value };
                      setRows(next);
                      setSaved(false);
                    }}
                    className="w-28 rounded-lg border border-black/10 px-2 py-1 text-right outline-none focus:border-brand"
                  />
                </td>
                <td className="px-3 py-1 text-right text-ink-soft">
                  {kopecks(Math.round((num(r.income) - num(r.expenses)) * 100))}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {localError && <p className="text-[14px] text-brand">{localError}</p>}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <button type="button" className={primary} onClick={() => void saveContent()}>
          Сохранить план
        </button>
        <button
          type="button"
          className={secondary}
          disabled={!saved}
          onClick={() => void confirm()}
          title={saved ? "" : "Сначала сохраните план"}
        >
          {saved ? "Подтвердить план" : "План не сохранён"}
        </button>
        {plan?.confirmed && <span className="text-[14px] text-ink-soft">План подтверждён ✓</span>}
      </div>
    </div>
  );
}
