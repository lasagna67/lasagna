"use client";

import { Button } from "@/components/ui/button";
import { ErrorBanner } from "@/components/ui/error-banner";
import { scenarioEmail, scenarios, type BusinessKind } from "@/lib/demo-data";

const plates = [
  "bg-plate-menthol hover:bg-[#b2e699]",
  "bg-plate-sonata hover:bg-[#8fd6f4]",
  "bg-plate-tomato hover:bg-[#ffc0bc]",
  "bg-plate-lagoon hover:bg-[#8ee8eb]",
  "bg-plate-chardonnay hover:bg-[#f7c384]",
  "bg-plate-pearl hover:bg-[#dfe3e3]",
] as const;

type Props = {
  kind: BusinessKind;
  busyKey: string | null;
  error: string;
  onPick: (key: string) => void;
  onRetry: (() => void) | null;
  onRegister: () => void;
  onBack: () => void;
};

export function ScenarioStep({ kind, busyKey, error, onPick, onRetry, onRegister, onBack }: Props) {
  return (
    <div className="mt-8">
      <div className="grid gap-4 sm:grid-cols-2">
        {scenarios.map((scenario, i) => (
          <button
            key={scenario.key}
            type="button"
            disabled={busyKey !== null}
            onClick={() => onPick(scenario.key)}
            className={`rounded-card-lg p-6 text-left transition-colors disabled:opacity-70 ${plates[i]}`}
          >
            <span className="block text-promo-s">{scenario.label}</span>
            <span className="mt-3 block text-[13px] text-ink-soft">
              {busyKey === scenario.key ? "Входим…" : scenarioEmail(kind, scenario.key)}
            </span>
          </button>
        ))}
      </div>

      <ErrorBanner message={error} onRetry={onRetry ?? undefined} className="mt-6" />

      <div className="mt-8 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <Button variant="ghost" onClick={onRegister}>
          Зарегистрировать нового пользователя
        </Button>
        <button type="button" onClick={onBack} className="text-[16px] text-ink-soft hover:text-brand">
          ← Сменить тип аккаунта
        </button>
      </div>
    </div>
  );
}
