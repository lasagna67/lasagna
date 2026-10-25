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
  // Ключи из editable_fields банка: в форме показываем и сохраняем только их.
  only?: Set<string>;
};

export function RequestForm({ app, token, error, onSave, only }: Props) {
  const r = app.request;
  const has = (key: string) => !only || only.has(key);
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
    if (has("request.purpose") && !purpose) return setLocalError("Выберите цель кредита");
    if (purpose === "other" && has("request.purpose_description") && !desc.trim())
      return setLocalError("Опишите цель кредита");
    if (has("request.amount_kopecks") && !(sum > 0)) return setLocalError("Укажите сумму больше нуля");
    if ((has("request.term_months") || has("request.need_period")) && !(termValue > 0))
      return setLocalError("Укажите срок");
    const request: Record<string, unknown> = {};
    if (has("request.purpose")) request.purpose = purpose;
    if (has("request.purpose_description"))
      request.purpose_description = !only && purpose !== "other" ? null : desc.trim() || null;
    if (has("request.amount_kopecks")) request.amount_kopecks = Math.round(sum * 100);
    if (has("request.need_period"))
      request.need_period =
        productCode === "overdraft" ? { unit: "days", value: termValue } : { unit: "months", value: termValue };
    if (has("request.product_code")) request.product_code = productCode || null;
    if (has("request.term_months")) request.term_months = productCode === "overdraft" ? null : termValue;
    if (has("request.collateral_mode")) request.collateral_mode = mode;
    if (Object.keys(request).length === 0) return true;
    return onSave({ request });
  }

  const showTerm = has("request.term_months") || has("request.need_period");

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        {has("request.purpose") &&
          label(
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
        {has("request.amount_kopecks") &&
          label(
            "Сумма, ₽",
            <input
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              inputMode="decimal"
              placeholder="3 000 000"
              className={input}
            />,
          )}
        {has("request.purpose_description") &&
          (purpose === "other" || !has("request.purpose")) &&
          label(
            "Пояснение цели",
            <input value={desc} onChange={(e) => setDesc(e.target.value)} className={input} />,
          )}
        {showTerm &&
          label(
            productCode === "overdraft" ? "Срок отдельного транша, дней" : "Срок, месяцев",
            <input
              value={term}
              onChange={(e) => setTerm(e.target.value)}
              inputMode="numeric"
              className={input}
            />,
          )}
        {has("request.collateral_mode") &&
          label(
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

      {(has("request.product_code") || has("request.collateral_mode")) && (
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
      )}

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
