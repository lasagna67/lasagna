"use client";

import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@/components/ui/button";

export type TourStep = { target?: string; title: string; text: ReactNode };

type Props = { steps: TourStep[]; onClose: () => void };

export function Tour({ steps, onClose }: Props) {
  const [index, setIndex] = useState(0);
  const [rect, setRect] = useState<DOMRect | null>(null);
  const step = steps[index];
  const last = index === steps.length - 1;
  const target = step.target;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    const findEl = () =>
      target ? document.querySelector<HTMLElement>(`[data-tour="${target}"]`) : null;
    const measure = () => {
      const el = findEl();
      if (!el) return setRect(null);
      el.scrollIntoView({
        block: "center",
        behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
      });
      setRect(el.getBoundingClientRect());
    };
    const track = () => {
      const el = findEl();
      if (el) setRect(el.getBoundingClientRect());
    };
    const t = setTimeout(measure, 0);
    window.addEventListener("resize", track);
    window.addEventListener("scroll", track, { passive: true });
    return () => {
      clearTimeout(t);
      window.removeEventListener("resize", track);
      window.removeEventListener("scroll", track);
    };
  }, [target]);

  return (
    <div className="fixed inset-0 z-90">
      <div className="absolute inset-0 bg-black/40" aria-hidden onClick={onClose} />
      {rect && (
        <div
          className="pointer-events-none fixed rounded-xl ring-2 ring-brand"
          style={{ top: rect.top - 6, left: rect.left - 6, width: rect.width + 12, height: rect.height + 12 }}
        />
      )}
      <div className="fixed inset-x-0 bottom-0 flex justify-center p-4 md:inset-x-auto md:bottom-6 md:left-1/2 md:w-[560px] md:-translate-x-1/2 md:p-0">
        <div
          className="w-full rounded-card-lg border border-black/8 bg-white p-5 md:p-6"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex items-start justify-between gap-4">
            <div>
              <span className="text-[13px] text-ink-soft">
                Шаг {index + 1} из {steps.length}
              </span>
              <h3 className="text-[18px] font-medium">{step.title}</h3>
            </div>
            <button
              type="button"
              aria-label="Закрыть туториал"
              onClick={onClose}
              className="-mt-1 flex size-8 shrink-0 items-center justify-center rounded-full text-[22px] leading-none text-ink-soft hover:bg-black/6"
            >
              ×
            </button>
          </div>
          <div className="mt-3 text-[15px] leading-6 text-ink-soft">{step.text}</div>
          <div className="mt-5 flex flex-wrap items-center gap-4">
            {index > 0 && (
              <button
                type="button"
                onClick={() => setIndex((i) => i - 1)}
                className="text-[15px] text-ink-soft hover:text-brand"
              >
                Назад
              </button>
            )}
            <Button size="m" onClick={() => (last ? onClose() : setIndex((i) => i + 1))}>
              {last ? "Завершить" : "Далее"}
            </Button>
            {!last && (
              <button
                type="button"
                onClick={onClose}
                className="text-[15px] text-ink-soft hover:text-brand"
              >
                Пропустить
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
