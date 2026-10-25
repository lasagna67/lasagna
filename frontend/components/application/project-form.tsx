"use client";

import { useState } from "react";
import { kopecks } from "@/lib/demo-data";
import type { Application } from "@/lib/types-application";
import { FieldErrors, errorFields, errorMessage, input, label, primary } from "./form-ui";

type Props = {
  app: Application;
  error: unknown;
  onSave: (body: Record<string, unknown>) => Promise<boolean>;
};

type Item = { name: string; amount: string };

export function ProjectForm({ app, error, onSave }: Props) {
  const p = app.product_details;
  const [description, setDescription] = useState(p?.project_description ?? "");
  const [items, setItems] = useState<Item[]>(
    p?.cost_items.length
      ? p.cost_items.map((i) => ({ name: i.name, amount: String(i.amount_kopecks / 100) }))
      : [{ name: "", amount: "" }],
  );
  const [ownFunds, setOwnFunds] = useState(p?.own_funds_kopecks != null ? String(p.own_funds_kopecks / 100) : "");
  const [salesStart, setSalesStart] = useState(p?.sales_start_month ?? "");
  const [localError, setLocalError] = useState("");

  const total = items.reduce((s, i) => s + (Number(i.amount.replace(",", ".")) || 0), 0);

  async function save() {
    setLocalError("");
    const rows = items.filter((i) => i.name.trim() || i.amount);
    if (rows.some((i) => !i.name.trim() || !(Number(i.amount.replace(",", ".")) > 0)))
      return setLocalError("У каждой статьи нужны название и сумма");
    if (!salesStart) return setLocalError("Укажите месяц начала продаж");
    await onSave({
      product_details: {
        project_description: description.trim() || null,
        cost_items: rows.map((i) => ({
          name: i.name.trim(),
          amount_kopecks: Math.round(Number(i.amount.replace(",", ".")) * 100),
        })),
        own_funds_kopecks: ownFunds ? Math.round(Number(ownFunds.replace(",", ".")) * 100) : null,
        sales_start_month: salesStart,
      },
    });
  }

  return (
    <div className="space-y-4">
      {label(
        "Описание проекта",
        <input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Пекарня у метро"
          className={input}
        />,
      )}

      <div>
        <span className="text-[14px] text-ink-soft">Статьи затрат</span>
        <div className="mt-2 space-y-2">
          {items.map((item, i) => (
            <div key={i} className="flex gap-2">
              <input
                value={item.name}
                placeholder="Название, например «Печь»"
                onChange={(e) =>
                  setItems(items.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))
                }
                className={input}
              />
              <input
                value={item.amount}
                inputMode="decimal"
                placeholder="Сумма, ₽"
                onChange={(e) =>
                  setItems(items.map((x, j) => (j === i ? { ...x, amount: e.target.value } : x)))
                }
                className={`${input} max-w-[160px]`}
              />
              <button
                type="button"
                aria-label="Убрать статью"
                onClick={() => setItems(items.filter((_, j) => j !== i))}
                className="shrink-0 rounded-2xl border border-black/10 px-3 text-ink-soft hover:border-brand"
              >
                ×
              </button>
            </div>
          ))}
        </div>
        <div className="mt-2 flex items-center gap-4">
          <button
            type="button"
            onClick={() => setItems([...items, { name: "", amount: "" }])}
            className="text-[14px] text-brand hover:underline"
          >
            + Добавить статью
          </button>
          <span className="text-[13px] text-ink-soft">Итого: {kopecks(Math.round(total * 100))}</span>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        {label(
          "Собственные средства, ₽",
          <input
            value={ownFunds}
            onChange={(e) => setOwnFunds(e.target.value)}
            inputMode="decimal"
            className={input}
          />,
        )}
        {label(
          "Начало продаж",
          <input
            type="month"
            value={salesStart}
            onChange={(e) => setSalesStart(e.target.value)}
            className={input}
          />,
        )}
      </div>

      {localError && <p className="text-[14px] text-brand">{localError}</p>}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}

      <button type="button" className={primary} onClick={() => void save()}>
        Сохранить сведения о проекте
      </button>
    </div>
  );
}
