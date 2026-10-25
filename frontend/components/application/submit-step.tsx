"use client";

import { useState } from "react";
import { api } from "@/lib/api";
import type { Application } from "@/lib/types-application";
import { AppSummary } from "./app-summary";
import { CsvFormat } from "./csv-format";
import { FieldErrors, errorFields, errorMessage, primary, secondary } from "./form-ui";

type Props = {
  app: Application;
  token: string;
  error: unknown;
  busy: boolean;
  onRefresh: () => Promise<void>;
  onSubmit: () => Promise<void>;
};

export function SubmitStep({ app, token, error, busy, onRefresh, onSubmit }: Props) {
  const fin = app.financials;
  const hasFinancials = fin.data_status !== "missing";
  const [csvBusy, setCsvBusy] = useState(false);
  const [csvError, setCsvError] = useState("");

  async function upload(file: File) {
    setCsvBusy(true);
    setCsvError("");
    try {
      await api.uploadAppCsv(app.id, file, app.revision, token);
    } catch (e) {
      setCsvError(errorMessage(e));
    } finally {
      setCsvBusy(false);
    }
    await onRefresh();
  }

  return (
    <div className="space-y-5">
      <AppSummary app={app} />
      {!hasFinancials && (
        <p className="rounded-2xl bg-plate-chardonnay/70 px-4 py-3 text-[14px] leading-5">
          Финансовые данные ещё не получены — без данных банк не примет заявку.
        </p>
      )}
      {!hasFinancials && fin.allow_csv_upload && (
        <div className="space-y-3">
          <label
            className={`${secondary} inline-flex cursor-pointer ${csvBusy ? "opacity-60" : ""}`}
          >
            {csvBusy ? "Загружаем…" : "Загрузить CSV с оборотами"}
            <input
              type="file"
              accept=".csv,text/csv"
              className="sr-only"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) void upload(file);
                e.target.value = "";
              }}
            />
          </label>
          <CsvFormat />
        </div>
      )}
      {!hasFinancials && !fin.allow_csv_upload && (
        <p className="text-[14px] text-ink-soft">
          Запросите данные на шаге «Финансы» — после неудачной попытки откроется загрузка CSV.
        </p>
      )}
      {csvError && <p className="text-[14px] text-brand">{csvError}</p>}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}
      <button type="button" className={primary} disabled={busy || csvBusy} onClick={() => void onSubmit()}>
        {busy ? "Отправляем…" : "Отправить заявку в банк"}
      </button>
    </div>
  );
}
