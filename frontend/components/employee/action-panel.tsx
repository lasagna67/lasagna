"use client";

import { useState } from "react";
import { ApiError, api, isRevisionConflict } from "@/lib/api";
import type { ReviewCard } from "@/lib/types-underwriter";
import {
  FieldErrors,
  errorFields,
  errorMessage,
  input,
  label,
  primary,
  secondary,
} from "@/components/application/form-ui";
import { DecisionForm } from "./decision-form";

type Props = {
  card: ReviewCard;
  token: string;
  onActed: () => Promise<void>;
  onConflict: () => void;
};

export const UW_ENDPOINT: Record<string, string> = {
  take: "take",
  request_info: "request-info",
  review_collateral: "collateral-review",
  review_cash_plan: "cash-plan-review",
  decide: "decision",
  retry_checks: "retry-checks",
  retry_scoring: "retry-scoring",
};

const EDITABLE: [string, string][] = [
  ["request.purpose", "Цель кредита"],
  ["request.purpose_description", "Описание цели"],
  ["request.amount_kopecks", "Сумма"],
  ["request.need_period", "Период"],
  ["request.product_code", "Продукт"],
  ["request.term_months", "Срок"],
  ["request.collateral_mode", "Режим обеспечения"],
  ["product_details", "Проект «Старт»"],
  ["collateral", "Залог"],
  ["cash_plan", "Денежный план"],
  ["financials", "Финансы"],
];

type Mode = "" | "request_info" | "collateral" | "cash_plan" | "decision" | "retry_checks";

export function ActionPanel({ card, token, onActed, onConflict }: Props) {
  const [mode, setMode] = useState<Mode>("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [message, setMessage] = useState("");
  const [fields, setFields] = useState<string[]>([]);
  const [coll, setColl] = useState({ result: "accepted", value: "", comment: "", public_comment: "" });
  const [plan, setPlan] = useState({ result: "accepted", reserve: "", comment: "", public_comment: "" });
  const [sources, setSources] = useState<string[]>(["registry", "credit_bureau"]);

  const actions = new Set(card.available_actions);

  async function act(action: string, body: Record<string, unknown> = {}) {
    setBusy(true);
    setError(null);
    try {
      await api.uwPost(
        card.id,
        UW_ENDPOINT[action],
        { expected_revision: card.revision, ...body },
        token,
      );
      setMode("");
      await onActed();
    } catch (e) {
      setError(e);
      if (isRevisionConflict(e)) onConflict();
    } finally {
      setBusy(false);
    }
  }

  const btn = (action: string, text: string, active: Mode) => (
    <button
      key={action}
      type="button"
      className={`${mode === active ? "bg-plate-sonata" : "bg-plate-pearl hover:bg-plate-sonata/60"} rounded-full px-4 py-2 text-[14px]`}
      disabled={busy}
      onClick={() => setMode(mode === active ? "" : active)}
    >
      {text}
    </button>
  );

  if (actions.size === 0) {
    return (
      <p className="rounded-card-lg border border-black/8 p-4 text-[14px] text-ink-soft">
        Нет действий: заявка не в статусе проверки или назначена другому сотруднику.
      </p>
    );
  }

  return (
    <section className="rounded-card-lg border border-black/8 p-5">
      <div className="flex flex-wrap gap-2">
        {actions.has("take") && (
          <button
            type="button"
            className="rounded-full bg-plate-menthol px-4 py-2 text-[14px] hover:opacity-90"
            disabled={busy}
            onClick={() => void act("take")}
          >
            Взять заявку
          </button>
        )}
        {actions.has("request_info") && btn("request_info", "Запросить уточнения", "request_info")}
        {actions.has("review_collateral") && btn("review_collateral", "Проверить залог", "collateral")}
        {actions.has("review_cash_plan") && btn("review_cash_plan", "Проверить план", "cash_plan")}
        {actions.has("decide") && btn("decide", "Решение", "decision")}
        {actions.has("retry_checks") && btn("retry_checks", "Повторить проверки", "retry_checks")}
        {actions.has("retry_scoring") && (
          <button
            type="button"
            className="rounded-full bg-plate-pearl px-4 py-2 text-[14px] hover:bg-plate-sonata/60"
            disabled={busy}
            onClick={() => void act("retry_scoring")}
          >
            Пересчитать скоринг
          </button>
        )}
      </div>

      <FieldErrors errors={errorFields(error)} />
      {error != null &&
        errorFields(error).length === 0 &&
        !(error instanceof ApiError && error.code === "INVALID_STATE") && (
          <p className="mt-3 text-[14px] text-brand">{errorMessage(error)}</p>
        )}
      {error instanceof ApiError && error.code === "INVALID_STATE" && (
        <p className="mt-3 text-[14px] text-brand">Действие недоступно в текущем статусе.</p>
      )}

      {mode === "request_info" && (
        <div className="mt-5 space-y-4">
          <label className="block">
            <span className="text-[14px] text-ink-soft">Что нужно уточнить у клиента</span>
            <textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={3}
              className={`${input} mt-1`}
              placeholder="Например: приложите счёт поставщика оборудования"
            />
          </label>
          <div>
            <p className="text-[14px] text-ink-soft">Что открыть для правки (необязательно)</p>
            <div className="mt-2 flex flex-wrap gap-2">
              {EDITABLE.map(([value, text]) => (
                <label
                  key={value}
                  className="flex items-center gap-2 rounded-xl border border-black/10 px-3 py-1.5 text-[13px]"
                >
                  <input
                    type="checkbox"
                    checked={fields.includes(value)}
                    onChange={(e) =>
                      setFields(e.target.checked ? [...fields, value] : fields.filter((f) => f !== value))
                    }
                    className="accent-[#ef3124]"
                  />
                  {text}
                </label>
              ))}
            </div>
          </div>
          <button
            type="button"
            className={primary}
            disabled={busy || !message.trim()}
            onClick={() => void act("request_info", { message, editable_fields: fields })}
          >
            {busy ? "Отправляем…" : "Отправить запрос"}
          </button>
        </div>
      )}

      {mode === "collateral" && (
        <div className="mt-5 grid gap-4 sm:grid-cols-2">
          {label(
            "Результат",
            <select
              value={coll.result}
              onChange={(e) => setColl({ ...coll, result: e.target.value })}
              className={input}
            >
              <option value="accepted">Принято</option>
              <option value="needs_info">Нужны уточнения</option>
              <option value="rejected">Отклонено</option>
            </select>,
          )}
          {coll.result === "accepted" &&
            label(
              "Принятая стоимость, ₽",
              <input
                value={coll.value}
                inputMode="decimal"
                onChange={(e) => setColl({ ...coll, value: e.target.value })}
                className={input}
              />,
            )}
          {label(
            "Внутренний комментарий (обязательно)",
            <input
              value={coll.comment}
              onChange={(e) => setColl({ ...coll, comment: e.target.value })}
              className={input}
            />,
          )}
          {label(
            "Комментарий клиенту",
            <input
              value={coll.public_comment}
              onChange={(e) => setColl({ ...coll, public_comment: e.target.value })}
              className={input}
            />,
          )}
          <div className="sm:col-span-2">
            <button
              type="button"
              className={primary}
              disabled={
                busy || !coll.comment.trim() ||
                (coll.result === "accepted" && !(Number(coll.value.replace(",", ".")) > 0))
              }
              onClick={() =>
                void act("review_collateral", {
                  result: coll.result,
                  accepted_value_kopecks:
                    coll.result === "accepted"
                      ? Math.round(Number(coll.value.replace(",", ".")) * 100)
                      : undefined,
                  comment: coll.comment,
                  public_comment: coll.public_comment || undefined,
                })
              }
            >
              {busy ? "Сохраняем…" : "Записать проверку"}
            </button>
          </div>
        </div>
      )}

      {mode === "cash_plan" && (
        <div className="mt-5 grid gap-4 sm:grid-cols-2">
          {label(
            "Результат",
            <select
              value={plan.result}
              onChange={(e) => setPlan({ ...plan, result: e.target.value })}
              className={input}
            >
              <option value="accepted">Принято</option>
              <option value="needs_info">Нужны уточнения</option>
              <option value="rejected">Отклонено</option>
            </select>,
          )}
          {plan.result === "accepted" &&
            label(
              "Проверенный резерв, ₽",
              <input
                value={plan.reserve}
                inputMode="decimal"
                onChange={(e) => setPlan({ ...plan, reserve: e.target.value })}
                className={input}
              />,
            )}
          {label(
            "Внутренний комментарий",
            <input
              value={plan.comment}
              onChange={(e) => setPlan({ ...plan, comment: e.target.value })}
              className={input}
            />,
          )}
          {label(
            "Комментарий клиенту",
            <input
              value={plan.public_comment}
              onChange={(e) => setPlan({ ...plan, public_comment: e.target.value })}
              className={input}
            />,
          )}
          <div className="sm:col-span-2">
            <button
              type="button"
              className={primary}
              disabled={
                busy ||
                (plan.result === "accepted" &&
                  (plan.reserve === "" || !(Number(plan.reserve.replace(",", ".")) >= 0)))
              }
              onClick={() =>
                void act("review_cash_plan", {
                  result: plan.result,
                  validated_reserve_kopecks:
                    plan.result === "accepted"
                      ? Math.round(Number(plan.reserve.replace(",", ".")) * 100)
                      : undefined,
                  comment: plan.comment,
                  public_comment: plan.public_comment || undefined,
                })
              }
            >
              {busy ? "Сохраняем…" : "Записать заключение"}
            </button>
            <p className="mt-2 text-[13px] text-ink-soft">
              Условия проверки берутся из заявки; резерв — от 0 до заявленного клиентом.
            </p>
          </div>
        </div>
      )}

      {mode === "retry_checks" && (
        <div className="mt-5 space-y-4">
          <div className="flex flex-wrap gap-3">
            {["registry", "credit_bureau"].map((s) => (
              <label key={s} className="flex items-center gap-2 text-[14px]">
                <input
                  type="checkbox"
                  checked={sources.includes(s)}
                  onChange={(e) =>
                    setSources(e.target.checked ? [...sources, s] : sources.filter((x) => x !== s))
                  }
                  className="accent-[#ef3124]"
                />
                {s === "registry" ? "Реестр" : "БКИ"}
              </label>
            ))}
          </div>
          <button
            type="button"
            className={secondary}
            disabled={busy || sources.length === 0}
            onClick={() => void act("retry_checks", { sources })}
          >
            {busy ? "Запрашиваем…" : "Повторить проверки"}
          </button>
          <p className="text-[13px] text-ink-soft">Результат придёт асинхронно с новой версией заявки.</p>
        </div>
      )}

      {mode === "decision" && (
        <DecisionForm card={card} busy={busy} error={error} onSubmit={(body) => act("decide", body)} />
      )}
    </section>
  );
}
