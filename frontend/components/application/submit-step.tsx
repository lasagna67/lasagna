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
  const canFetch = app.available_actions.includes("fetch_financials");
  const [csvBusy, setCsvBusy] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [actionError, setActionError] = useState("");

  async function upload(file: File) {
    setCsvBusy(true);
    setActionError("");
    try {
      await api.uploadAppCsv(app.id, file, app.revision, token);
    } catch (e) {
      setActionError(errorMessage(e));
    } finally {
      setCsvBusy(false);
    }
    await onRefresh();
  }

  async function fetchSource() {
    setFetching(true);
    setActionError("");
    try {
      await api.fetchFinancials(app.id, { expected_revision: app.revision }, token);
    } catch (e) {
      setActionError(errorMessage(e));
    } finally {
      setFetching(false);
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
          <label className={`${secondary} inline-flex cursor-pointer ${csvBusy ? "opacity-60" : ""}`}>
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
      {!hasFinancials && !fin.allow_csv_upload && canFetch && (
        <div className="space-y-2">
          <button
            type="button"
            className={secondary}
            disabled={fetching || busy}
            onClick={() => void fetchSource()}
          >
            {fetching ? "Запрашиваем…" : "Получить данные из источника"}
          </button>
          <p className="text-[13px] text-ink-soft">
            Если источник не ответит — здесь же появится загрузка CSV.
          </p>
        </div>
      )}
      {!hasFinancials && !fin.allow_csv_upload && !canFetch && (
        <p className="text-[14px] text-ink-soft">
          Источник данных недоступен — загрузите CSV на шаге «Финансы».
        </p>
      )}
      {actionError && <p className="text-[14px] text-brand">{actionError}</p>}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}
      <button
        type="button"
        className={primary}
        disabled={busy || csvBusy || fetching || !hasFinancials}
        onClick={() => void onSubmit()}
      >
        {busy ? "Отправляем…" : "Отправить заявку в банк"}
      </button>
    </div>
  );
}
