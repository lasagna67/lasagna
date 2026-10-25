"use client";

import { useState } from "react";
import { kopecks, purposeLabel, purposes } from "@/lib/demo-data";
import type { Application } from "@/lib/types-application";
import { ProductPicker } from "./product-picker";
import { FieldErrors, errorFields, errorMessage, input, label, primary, select } from "./form-ui";

type Props = {
  app: Application;
  token: string;
  error: unknown;
  onSave: (body: Record<string, unknown>) => Promise<boolean>;
};

export function RequestForm({ app, token, error, onSave }: Props) {
  const r = app.request;
  const [purpose, setPurpose] = useState(r.purpose ?? "");
  const [desc, setDesc] = useState(r.purpose_description ?? "");
  const [amount, setAmount] = useState(r.amount_kopecks != null ? String(r.amount_kopecks / 100) : "");
  const [term, setTerm] = useState(
    r.term_months != null ? String(r.term_months) : (r.need_period.value != null ? String(r.need_period.value) : ""),
  );
  const [productCode, setProductCode] = useState(r.product_code ?? "");
  const [mode, setMode] = useState<"none" | "pledged">(r.collateral_mode);
  const [localError, setLocalError] = useState("");

  async function save() {
    setLocalError("");
    const sum = Number(amount.replace(",", "."));
    const termValue = Number(term);
    if (!purpose) return setLocalError("Выберите цель кредита");
    if (purpose === "other" && !desc.trim()) return setLocalError("Опишите цель кредита");
    if (!(sum > 0)) return setLocalError("Укажите сумму больше нуля");
    if (!(termValue > 0)) return setLocalError("Укажите срок");
    const ok = await onSave({
      request: {
        purpose,
        purpose_description: purpose === "other" ? desc.trim() : null,
        amount_kopecks: Math.round(sum * 100),
        need_period:
          productCode === "overdraft"
            ? { unit: "days", value: termValue }
            : { unit: "months", value: termValue },
        product_code: productCode || null,
        term_months: productCode === "overdraft" ? null : termValue,
        collateral_mode: mode,
      },
    });
    if (!ok) return;
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        {label(
          "Цель кредита",
          <>
            <select value={purpose} onChange={(e) => setPurpose(e.target.value)} className={select}>
              <option value="">Выберите цель</option>
              {Object.entries(purposes).map(([key, name]) => (
                <option key={key} value={key}>
                  {name}
                </option>
              ))}
            </select>
          </>,
        )}
        {label(
          "Сумма, ₽",
          <input
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            inputMode="decimal"
            placeholder="3 000 000"
            className={input}
          />,
        )}
        {purpose === "other" &&
          label(
            "Пояснение цели",
            <input value={desc} onChange={(e) => setDesc(e.target.value)} className={input} />,
          )}
        {label(
          productCode === "overdraft" ? "Срок отдельного транша, дней" : "Срок, месяцев",
          <input
            value={term}
            onChange={(e) => setTerm(e.target.value)}
            inputMode="numeric"
            className={input}
          />,
        )}
        {label(
          "Обеспечение",
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as "none" | "pledged")}
            className={select}
          >
            <option value="none">Без залога</option>
            <option value="pledged">С залогом</option>
          </select>,
        )}
      </div>

      <ProductPicker
        purpose={purpose}
        amount={amount}
        term={term}
        token={token}
        value={productCode}
        mode={mode}
        onPick={(code, next) => {
          setProductCode(code);
          setMode(next);
        }}
      />

      {localError && <p className="text-[14px] text-brand">{localError}</p>}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}

      <button type="button" className={primary} onClick={() => void save()}>
        Сохранить и дальше
      </button>
      <span className="ml-3 text-[13px] text-ink-soft">
        {amount && Number(amount) > 0 && `сумма: ${kopecks(Math.round(Number(amount.replace(",", ".")) * 100))}`}{" "}
        {purpose && `· ${purposeLabel(purpose)}`}
      </span>
    </div>
  );
}
