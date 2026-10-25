"use client";

import { useState } from "react";
import type { Application, Collateral } from "@/lib/types-application";
import { FieldErrors, errorFields, errorMessage, input, label, primary, select } from "./form-ui";

type Props = {
  app: Application;
  error: unknown;
  onSave: (body: Record<string, unknown>) => Promise<boolean>;
};

export function CollateralForm({ app, error, onSave }: Props) {
  const c = app.collateral;
  const [type, setType] = useState(c?.type ?? "equipment");
  const [ownership, setOwnership] = useState(c?.ownership ?? "existing");
  const [description, setDescription] = useState(c?.description ?? "");
  const [value, setValue] = useState(c ? String(c.declared_value_kopecks / 100) : "");
  const [localError, setLocalError] = useState("");

  async function save() {
    setLocalError("");
    const sum = Number(value.replace(",", "."));
    if (!description.trim()) return setLocalError("Опишите предмет залога");
    if (!(sum > 0)) return setLocalError("Укажите заявленную стоимость");
    await onSave({
      collateral: {
        type,
        ownership,
        description: description.trim(),
        declared_value_kopecks: Math.round(sum * 100),
      },
    });
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        {label(
          "Предмет залога",
          <select
            value={type}
            onChange={(e) => setType(e.target.value as Collateral["type"])}
            className={select}
          >
            <option value="real_estate">Недвижимость</option>
            <option value="equipment">Оборудование</option>
            <option value="vehicle">Транспорт</option>
          </select>,
        )}
        {label(
          "Право",
          <select
            value={ownership}
            onChange={(e) => setOwnership(e.target.value as Collateral["ownership"])}
            className={select}
          >
            <option value="existing">Уже есть</option>
            <option value="acquired">Будет приобретено</option>
          </select>,
        )}
        {label(
          "Описание",
          <input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Токарный станок"
            className={input}
          />,
        )}
        {label(
          "Заявленная стоимость, ₽",
          <input
            value={value}
            onChange={(e) => setValue(e.target.value)}
            inputMode="decimal"
            placeholder="5 000 000"
            className={input}
          />,
        )}
      </div>

      {localError && <p className="text-[14px] text-brand">{localError}</p>}
      <FieldErrors errors={errorFields(error)} />
      {error != null && errorFields(error).length === 0 && (
        <p className="text-[14px] text-brand">{errorMessage(error)}</p>
      )}

      <button type="button" className={primary} onClick={() => void save()}>
        Сохранить залог
      </button>
    </div>
  );
}
