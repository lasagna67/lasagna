"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ProductSuggestion } from "@/lib/types-application";
import { kopecks } from "@/lib/demo-data";

type Props = {
  purpose: string;
  amount: string;
  term: string;
  token: string;
  value: string;
  mode: "none" | "pledged";
  onPick: (code: string, mode: "none" | "pledged") => void;
};

const eligibilityPlate: Record<string, string> = {
  eligible: "bg-plate-menthol",
  needs_details: "bg-plate-chardonnay",
  ineligible: "bg-plate-tomato",
};

const eligibilityLabel: Record<string, string> = {
  eligible: "Подходит",
  needs_details: "Нужны данные",
  ineligible: "Не подходит",
};

export function ProductPicker({ purpose, amount, term, token, value, mode, onPick }: Props) {
  const [items, setItems] = useState<ProductSuggestion[]>([]);

  useEffect(() => {
    let alive = true;
    const amountKopecks = Number(amount) > 0 ? Math.round(Number(amount) * 100) : undefined;
    const termMonths = Number(term) > 0 ? Number(term) : undefined;
    const params: Record<string, string | number | undefined> = {
      purpose: purpose || undefined,
      amount_kopecks: amountKopecks,
    };
    params[termMonths ? "term_months" : "need_period_days"] = termMonths;
    api
      .products(params, token)
      .then((list) => alive && setItems(list))
      .catch(() => alive && setItems([]));
    return () => {
      alive = false;
    };
  }, [purpose, amount, term, token]);

  if (!items.length) return null;

  return (
    <div className="mt-6">
      <span className="text-[14px] text-ink-soft">Подходящие продукты</span>
      <div className="mt-2 grid gap-3 sm:grid-cols-2">
        {items.map(({ product, eligibility, reasons }) => (
          <button
            key={product.code}
            type="button"
            onClick={() =>
              onPick(
                product.code,
                product.collateral_mode === "required"
                  ? "pledged"
                  : product.collateral_mode === "not_supported"
                    ? "none"
                    : mode,
              )
            }
            className={`rounded-2xl border p-4 text-left transition-colors ${product.code === value ? "border-brand bg-brand/4" : "border-black/10 hover:border-brand/60"}`}
          >
            <span className="flex items-center justify-between gap-2">
              <span className="text-[16px] font-medium">{product.name}</span>
              <span className={`shrink-0 rounded-full px-2 py-0.5 text-[12px] ${eligibilityPlate[eligibility]}`}>
                {eligibilityLabel[eligibility]}
              </span>
            </span>
            <span className="mt-1 block text-[13px] text-ink-soft">
              {kopecks(product.min_amount_kopecks)} — {kopecks(product.max_amount_kopecks)} ·{" "}
              {product.min_term_months}–{product.max_term_months} мес.
              {product.base_rate != null && ` · от ${product.base_rate}%`}
            </span>
            {reasons.length > 0 && (
              <span className="mt-1 block text-[13px] text-ink-soft">{reasons[0].message}</span>
            )}
          </button>
        ))}
      </div>
    </div>
  );
}
