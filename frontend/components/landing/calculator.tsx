"use client";

import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";

const RATE = 15.9;
const MIN_AMOUNT = 500_000;
const MAX_AMOUNT = 30_000_000;
const MIN_MONTHS = 6;
const MAX_MONTHS = 60;

const money = new Intl.NumberFormat("ru-RU", {
  style: "currency",
  currency: "RUB",
  maximumFractionDigits: 0,
});

function pluralMonths(n: number) {
  const d10 = n % 10;
  const d100 = n % 100;
  if (d10 === 1 && d100 !== 11) return "месяц";
  if (d10 >= 2 && d10 <= 4 && (d100 < 12 || d100 > 14)) return "месяца";
  return "месяцев";
}

function monthlyPayment(amount: number, months: number, rate: number) {
  const i = rate / 100 / 12;
  return (amount * i) / (1 - Math.pow(1 + i, -months));
}

export function Calculator() {
  const [amount, setAmount] = useState(3_000_000);
  const [months, setMonths] = useState(24);
  const payment = useMemo(() => monthlyPayment(amount, months, RATE), [amount, months]);

  return (
    <section id="calculator" className="scroll-mt-24">
      <Container>
        <h2 className="text-promo-m">Калькулятор платежа</h2>
        <div className="mt-8 grid gap-4 md:grid-cols-2">
          <div className="rounded-card bg-plate-pearl p-8">
            <label className="block">
              <span className="flex items-baseline justify-between gap-4 text-[16px]">
                <span>Сумма кредита</span>
                <span className="font-semibold">{money.format(amount)}</span>
              </span>
              <input
                type="range"
                min={MIN_AMOUNT}
                max={MAX_AMOUNT}
                step={100_000}
                value={amount}
                onChange={(e) => setAmount(Number(e.target.value))}
                className="mt-3 w-full accent-brand"
              />
              <span className="mt-1 flex justify-between text-[12px] text-ink-soft">
                <span>{money.format(MIN_AMOUNT)}</span>
                <span>{money.format(MAX_AMOUNT)}</span>
              </span>
            </label>
            <label className="mt-10 block">
              <span className="flex items-baseline justify-between gap-4 text-[16px]">
                <span>Срок кредита</span>
                <span className="font-semibold">
                  {months} {pluralMonths(months)}
                </span>
              </span>
              <input
                type="range"
                min={MIN_MONTHS}
                max={MAX_MONTHS}
                step={1}
                value={months}
                onChange={(e) => setMonths(Number(e.target.value))}
                className="mt-3 w-full accent-brand"
              />
              <span className="mt-1 flex justify-between text-[12px] text-ink-soft">
                <span>
                  {MIN_MONTHS} {pluralMonths(MIN_MONTHS)}
                </span>
                <span>
                  {MAX_MONTHS} {pluralMonths(MAX_MONTHS)}
                </span>
              </span>
            </label>
          </div>
          <div className="flex flex-col rounded-card bg-plate-sonata p-8">
            <p className="text-[16px]">Ежемесячный платёж</p>
            <p className="mt-3 text-promo-l font-medium">{money.format(Math.round(payment))}</p>
            <p className="mt-2 text-[14px] text-ink-soft">
              при ставке {RATE}% годовых, аннуитетные платежи
            </p>
            <Button href="#apply" className="mt-auto self-start" size="l">
              Оставить заявку
            </Button>
            <p className="mt-4 text-[12px] leading-4 text-ink-soft">
              Расчёт предварительный и не является офертой. Точные условия — после проверки
              данных.
            </p>
          </div>
        </div>
      </Container>
    </section>
  );
}
