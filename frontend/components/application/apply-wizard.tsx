"use client";

import { useState } from "react";
import { ApiError, api, isRevisionConflict } from "@/lib/api";
import type { Application } from "@/lib/types-application";
import { CashPlanForm } from "./cash-plan-form";
import { CollateralForm } from "./collateral-form";
import { FinancialsPanel } from "./financials-panel";
import { ProjectForm } from "./project-form";
import { RequestForm } from "./request-form";
import { SubmitStep } from "./submit-step";

type Props = {
  app: Application;
  token: string;
  onUpdated: (app: Application) => void;
  onConflict: () => void;
  onRefresh: () => Promise<void>;
};

const stepChip =
  "rounded-full px-3 py-1 text-[13px] transition-colors disabled:opacity-50";
const activeChip = "bg-plate-sonata font-medium";

export function ApplyWizard({ app, token, onUpdated, onConflict, onRefresh }: Props) {
  const [step, setStep] = useState(0);
  const [saving, setSaving] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const isStart = app.request.product_code === "start";

  async function patch(body: Record<string, unknown>, advance: boolean): Promise<boolean> {
    setSaving(true);
    setError(null);
    try {
      const next = await api.patchApp(
        app.id,
        { expected_revision: app.revision, ...body },
        token,
      );
      onUpdated(next);
      if (advance) setStep((s) => s + 1);
      return true;
    } catch (e) {
      setError(e);
      if (isRevisionConflict(e)) onConflict();
      return false;
    } finally {
      setSaving(false);
    }
  }

  async function submit() {
    setSubmitting(true);
    setError(null);
    try {
      onUpdated(await api.submitApp(app.id, app.revision, token));
    } catch (e) {
      setError(e);
      if (isRevisionConflict(e)) onConflict();
    } finally {
      setSubmitting(false);
    }
  }

  const steps: { key: string; title: string; node: React.ReactNode; next?: boolean }[] = [
    {
      key: "request",
      title: "Параметры",
      node: <RequestForm app={app} token={token} error={error} onSave={(b) => patch(b, true)} />,
    },
    ...(app.request.collateral_mode === "pledged"
      ? [
          {
            key: "collateral",
            title: "Залог",
            node: <CollateralForm app={app} error={error} onSave={(b) => patch(b, true)} />,
          },
        ]
      : []),
    ...(isStart
      ? [
          {
            key: "project",
            title: "Проект",
            node: <ProjectForm app={app} error={error} onSave={(b) => patch(b, true)} />,
          },
          {
            key: "cash",
            title: "Денежный план",
            node: <CashPlanForm app={app} error={error} onSave={(b) => patch(b, false)} />,
            next: Boolean(app.cash_plan),
          },
        ]
      : []),
    {
      key: "financials",
      title: "Финансы",
      node: (
        <FinancialsPanel
          app={app}
          token={token}
          error={error}
          onDone={async () => {
            await onRefresh();
            return true;
          }}
        />
      ),
      next: true,
    },
    {
      key: "submit",
      title: "Отправка",
      node: (
        <SubmitStep
          app={app}
          token={token}
          error={error}
          busy={submitting}
          onRefresh={onRefresh}
          onSubmit={submit}
        />
      ),
    },
  ];
  const current = steps[Math.min(step, steps.length - 1)];

  return (
    <section className="mt-8 rounded-card-lg border border-black/8 p-6 md:p-8">
      <div className="flex flex-wrap gap-2">
        {steps.map((s, i) => (
          <button
            key={s.key}
            type="button"
            disabled={i > step || saving}
            onClick={() => setStep(i)}
            className={`${stepChip} ${i === step ? activeChip : "bg-plate-pearl text-ink-soft hover:text-ink"}`}
          >
            {i + 1}. {s.title}
          </button>
        ))}
      </div>

      <div className="mt-6" key={current.key}>
        {current.node}
      </div>

      {current.key !== "submit" && (
        <div className="mt-6 flex items-center gap-4">
          {step > 0 && (
            <button
              type="button"
              disabled={saving}
              onClick={() => setStep((s) => s - 1)}
              className="text-[15px] text-ink-soft hover:text-brand"
            >
              ← Назад
            </button>
          )}
          {current.next && step < steps.length - 1 && (
            <button
              type="button"
              disabled={saving}
              onClick={() => setStep((s) => s + 1)}
              className="text-[15px] text-brand hover:underline"
            >
              Дальше →
            </button>
          )}
        </div>
      )}

      {error instanceof ApiError && error.code === "REVISION_CONFLICT" && (
        <p className="mt-4 text-[14px] text-brand">Данные изменились — обновите страницу.</p>
      )}
      {saving && <p className="mt-4 text-[13px] text-ink-soft">Сохраняем…</p>}
    </section>
  );
}
