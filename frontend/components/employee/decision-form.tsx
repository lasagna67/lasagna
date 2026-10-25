"use client";

import { useState } from "react";
import { kopecks } from "@/lib/demo-data";
import type { ReviewCard } from "@/lib/types-underwriter";
import {
  FieldErrors,
  errorFields,
  errorMessage,
  input,
  label,
  primary,
} from "@/components/application/form-ui";

type Props = {
  card: ReviewCard;
  busy: boolean;
  error: unknown;
  onSubmit: (body: Record<string, unknown>) => Promise<void>;
};

export function DecisionForm({ card, busy, error, onSubmit }: Props) {
  const [outcome, setOutcome] = useState("approved");
  const [productCode, setProductCode] = useState(card.request.product_code ?? "");
  const [amount, setAmount] = useState(
    card.request.amount_kopecks != null ? String(card.request.amount_kopecks / 100) : "",
  );
  const [term, setTerm] = useState(card.request.term_months != null ? String(card.request.term_months) : "");
  const [rate, setRate] = useState(card.offer ? String(card.offer.annual_rate_bps / 100) : "");
  const [agreementTerm, setAgreementTerm] = useState("12");
  const [trancheDays, setTrancheDays] = useState("60");
  const [reasonCode, setReasonCode] = useState("");
  const [reasons, setReasons] = useState("");
  const [comment, setComment] = useState("");
  const [localError, setLocalError] = useState("");

  const blockers = card.decision_blockers;
  const positive = outcome !== "rejected";
  const blocked = positive && blockers.length > 0;
  const rejectedReady = Boolean(reasonCode.trim() && reasons.trim());
  const overdraft = productCode === "overdraft";

  function submit() {
    setLocalError("");
    if (blocked || (outcome === "rejected" && !rejectedReady)) return;
    const rateValue = Number(rate.replace(",", "."));
    if (positive && !(rateValue >= 0)) return setLocalError("Укажите ставку, % годовых");
    const termValue = Number(term);
    if (positive && !overdraft && !(termValue > 0)) return setLocalError("Укажите срок кредита");
    void onSubmit({
      outcome,
      ...(positive
        ? {
            terms: {
              product_code: productCode || undefined,
              amount_kopecks: Math.round((Number(amount.replace(",", ".")) || 0) * 100),
              annual_rate_bps: Math.round(rateValue * 100),
              ...(overdraft
                ? {
                    agreement_term_months: Number(agreementTerm) || undefined,
                    max_tranche_days: Number(trancheDays) || undefined,
                  }
                : { term_months: termValue }),
            },
          }
        : {}),
      ...(outcome === "rejected"
        ? {
            reason_code: reasonCode.trim(),
            reasons: reasons
              .split("\n")
              .map((s) => s.trim())
              .filter(Boolean),
          }
        : {}),
      ...(comment.trim() ? { comment: comment.trim() } : {}),
    });
  }

  const outcomes: [string, string][] = [
    ["approved", "Одобрить"],
    ["counter_offer", "Контрпредложение"],
    ["rejected", "Отклонить"],
  ];

  return (
    <div className="mt-5 space-y-4">
      <div className="flex flex-wrap gap-2">
        {outcomes.map(([value, text]) => (
          <label
            key={value}
            className={`flex cursor-pointer items-center gap-2 rounded-full px-4 py-2 text-[14px] ${outcome === value ? "bg-plate-sonata" : "bg-plate-pearl hover:bg-plate-sonata/60"}`}
          >
            <input
              type="radio"
              name="outcome"
              value={value}
              checked={outcome === value}
              onChange={() => setOutcome(value)}
              className="accent-[#ef3124]"
            />
            {text}
          </label>
        ))}
      </div>

      {blockers.length > 0 && (
        <ul className="rounded-2xl bg-plate-chardonnay/70 px-4 py-3 text-[14px] leading-5">
          <li className="font-medium">Положительное решение сейчас заблокировано:</li>
          {blockers.map((b, i) => (
            <li key={i}>— {b.message}</li>
          ))}
        </ul>
      )}

      {positive && outcome === "approved" && (
        <p className="rounded-2xl bg-plate-pearl px-4 py-3 text-[14px]">
          Условия как в запросе клиента: сумма{" "}
          {card.request.amount_kopecks != null ? kopecks(card.request.amount_kopecks) : "—"}, срок{" "}
          {card.request.term_months ? `${card.request.term_months} мес.` : "—"}, продукт{" "}
          {card.request.product_code ?? "—"}.
        </p>
      )}

      {positive && outcome === "approved" && (
        <div className="max-w-[240px]">
          {label(
            "Ставка, % годовых",
            <input
              value={rate}
              onChange={(e) => setRate(e.target.value)}
              inputMode="decimal"
              placeholder="19.9"
              className={input}
            />,
          )}
        </div>
      )}

      {outcome === "counter_offer" && (
        <div className="grid gap-4 sm:grid-cols-3">
          {label(
            "Продукт",
            <input value={productCode} onChange={(e) => setProductCode(e.target.value)} className={input} />,
          )}
          {label(
            "Сумма, ₽",
            <input
              value={amount}
              inputMode="decimal"
              onChange={(e) => setAmount(e.target.value)}
              className={input}
            />,
          )}
          {overdraft ? (
            <>
              {label(
                "Срок соглашения, мес.",
                <input
                  value={agreementTerm}
                  inputMode="numeric"
                  onChange={(e) => setAgreementTerm(e.target.value)}
                  className={input}
                />,
              )}
              {label(
                "Транш, дней",
                <input
                  value={trancheDays}
                  inputMode="numeric"
                  onChange={(e) => setTrancheDays(e.target.value)}
                  className={input}
                />,
              )}
            </>
          ) : (
            label(
              "Срок, мес.",
              <input value={term} inputMode="numeric" onChange={(e) => setTerm(e.target.value)} className={input} />,
            )
          )}
          {label(
            "Ставка, % годовых",
            <input
              value={rate}
              onChange={(e) => setRate(e.target.value)}
              inputMode="decimal"
              placeholder="19.9"
              className={input}
            />,
          )}
          <p className="text-[13px] text-ink-soft sm:col-span-3">
            Платёж рассчитает сервер. Для контрпредложения параметры должны отличаться от запроса
            клиента.
          </p>
        </div>
      )}

      {outcome === "rejected" && (
        <div className="space-y-4">
          {label(
            "Код причины, например HIGH_DEBT_LOAD",
            <input
              value={reasonCode}
              onChange={(e) => setReasonCode(e.target.value)}
              className={input}
            />,
          )}
          <label className="block">
            <span className="text-[14px] text-ink-soft">Пояснения для клиента (каждая строка — пункт)</span>
            <textarea
              value={reasons}
              onChange={(e) => setReasons(e.target.value)}
              rows={3}
              className={`${input} mt-1`}
            />
          </label>
        </div>
      )}

      {label(
        "Внутренний комментарий (клиенту не показывается)",
        <input value={comment} onChange={(e) => setComment(e.target.value)} className={input} />,
      )}

      {localError && <p className="text-[14px] text-brand">{localError}</p>}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="mt-3 text-[14px] text-brand">{errorMessage(error)}</p>
      )}

      <button
        type="button"
        className={primary}
        disabled={busy || blocked || (outcome === "rejected" && !rejectedReady)}
        onClick={submit}
      >
        {busy ? "Публикуем…" : "Опубликовать решение"}
      </button>
      <p className="text-[13px] text-ink-soft">
        Одобрение и контрпредложение создают предложение и PDF договора. Решение видно клиенту в
        кабинете.
      </p>
    </div>
  );
}
