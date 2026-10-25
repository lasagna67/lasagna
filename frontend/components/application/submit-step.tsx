"use client";

import { AppSummary } from "./app-summary";
import { FieldErrors, errorFields, errorMessage, primary } from "./form-ui";
import type { Application } from "@/lib/types-application";

type Props = {
  app: Application;
  error: unknown;
  busy: boolean;
  onSubmit: () => Promise<void>;
};

export function SubmitStep({ app, error, busy, onSubmit }: Props) {
  const hasFinancials = app.financials.data_status !== "missing";
  return (
    <div className="space-y-5">
      <AppSummary app={app} />
      {!hasFinancials && (
        <p className="rounded-2xl bg-plate-chardonnay/70 px-4 py-3 text-[14px] leading-5">
          Финансовые данные ещё не получены — на шаге «Финансы» запросите их из источника или
          загрузите CSV. Без данных банк может не принять заявку.
        </p>
      )}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}
      <button type="button" className={primary} disabled={busy} onClick={() => void onSubmit()}>
        {busy ? "Отправляем…" : "Отправить заявку в банк"}
      </button>
    </div>
  );
}
