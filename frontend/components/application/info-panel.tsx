"use client";

import { useState } from "react";
import { ApiError, api, isRevisionConflict } from "@/lib/api";
import { dateRu } from "@/lib/demo-data";
import type { Application, InfoRequest } from "@/lib/types-application";
import { AppSummary } from "./app-summary";
import { CashPlanForm } from "./cash-plan-form";
import { CollateralForm } from "./collateral-form";
import { FinancialsPanel } from "./financials-panel";
import { ProjectForm } from "./project-form";
import { RequestForm } from "./request-form";
import { FieldErrors, errorFields, errorMessage, input, primary, secondary } from "./form-ui";

type Props = {
  app: Application;
  token: string;
  requests: InfoRequest[];
  onUpdated: (app: Application) => void;
  onConflict: () => void;
  onRefresh: () => Promise<void>;
};

export function InfoPanel({ app, token, requests, onUpdated, onConflict, onRefresh }: Props) {
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const editable = new Set(app.editable_fields);

  async function patch(body: Record<string, unknown>): Promise<boolean> {
    setBusy(true);
    setError(null);
    try {
      onUpdated(await api.patchApp(app.id, { expected_revision: app.revision, ...body }, token));
      return true;
    } catch (e) {
      setError(e);
      if (isRevisionConflict(e)) onConflict();
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function answer(req: InfoRequest) {
    setBusy(true);
    setError(null);
    try {
      onUpdated(
        await api.answerInfo(
          app.id,
          req.id,
          { expected_revision: app.revision, answer: answers[req.id] ?? "" },
          token,
        ),
      );
      await onRefresh();
    } catch (e) {
      setError(e);
      if (isRevisionConflict(e)) onConflict();
    } finally {
      setBusy(false);
    }
  }

  async function resubmit() {
    setBusy(true);
    setError(null);
    try {
      onUpdated(await api.resubmitApp(app.id, app.revision, token));
    } catch (e) {
      setError(e);
      if (isRevisionConflict(e)) onConflict();
    } finally {
      setBusy(false);
    }
  }

  const open = requests.filter((r) => r.status === "open");
  const answered = requests.filter((r) => r.status === "answered");
  const canResubmit = app.available_actions.includes("resubmit") && open.length === 0;

  return (
    <div className="mt-8 space-y-8">
      {requests.map((req) => (
        <section key={req.id} className="rounded-card-lg border border-black/8 p-6">
          <div className="flex flex-wrap items-center gap-3">
            <h3 className="text-[16px] font-medium">Банк запросил уточнения</h3>
            {req.status === "answered" && (
              <span className="rounded-full bg-plate-menthol px-3 py-1 text-[13px]">
                Ответ отправлен {dateRu(req.answered_at)}
              </span>
            )}
          </div>
          <p className="mt-2 text-[15px] leading-6">{req.message}</p>
          <p className="mt-2 text-[13px] text-ink-soft">
            от {dateRu(req.requested_at)} · правка доступна:{" "}
            {req.editable_fields.join(", ") || "—"}
          </p>
          {req.status === "open" ? (
            <>
              <textarea
                value={answers[req.id] ?? ""}
                onChange={(e) => setAnswers({ ...answers, [req.id]: e.target.value })}
                placeholder="Ваш ответ банку"
                rows={3}
                className={`${input} mt-4`}
              />
              <button
                type="button"
                className={`${primary} mt-3`}
                disabled={busy || !(answers[req.id] ?? "").trim()}
                onClick={() => void answer(req)}
              >
                {busy ? "Отправляем…" : "Ответить"}
              </button>
            </>
          ) : (
            <p className="mt-3 rounded-2xl bg-plate-pearl px-4 py-3 text-[15px]">
              {req.answer || "—"}
            </p>
          )}
        </section>
      ))}

      <section className="rounded-card-lg border border-black/8 p-6">
        <h3 className="text-[16px] font-medium">Параметры, доступные для правки</h3>
        <div className="mt-4 space-y-6">
          <AppSummary app={app} />
          {[...editable].some((f) => f.startsWith("request.")) && (
            <RequestForm app={app} token={token} error={error} onSave={patch} only={editable} />
          )}
          {editable.has("collateral") && (
            <CollateralForm app={app} error={error} onSave={patch} />
          )}
          {editable.has("product_details") && (
            <ProjectForm app={app} error={error} onSave={patch} />
          )}
          {editable.has("cash_plan") && (
            <CashPlanForm app={app} error={error} onSave={(b) => patch(b)} />
          )}
          {editable.has("financials") && (
            <FinancialsPanel
              app={app}
              token={token}
              error={error}
              onDone={async () => {
                await onRefresh();
                return true;
              }}
            />
          )}
        </div>
      </section>

      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}
      {error instanceof ApiError && error.code === "INVALID_STATE" && (
        <p className="text-[14px] text-brand">Сначала ответьте на все запросы банка.</p>
      )}

      <div className="flex items-center gap-4">
        <button
          type="button"
          className={secondary}
          disabled={!canResubmit || busy}
          onClick={() => void resubmit()}
          title={open.length ? "Сначала ответьте на открытые запросы" : ""}
        >
          {busy ? "Отправляем…" : "Отправить заявку повторно"}
        </button>
        {open.length > 0 ? (
          <span className="text-[14px] text-ink-soft">Осталось ответить: {open.length}</span>
        ) : (
          answered.length > 0 && (
            <span className="text-[14px] text-ink-soft">
              Все запросы отвечены — отправьте заявку повторно.
            </span>
          )
        )}
      </div>
    </div>
  );
}
