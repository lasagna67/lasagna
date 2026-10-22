"use client";

import { useEffect } from "react";
import type { ReactNode } from "react";

type Props = {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
};

export function Modal({ title, onClose, children, footer }: Props) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-100 flex items-end justify-center bg-black/40 p-4 md:items-center"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={onClose}
    >
      <div
        className="max-h-[90vh] w-full max-w-[560px] overflow-y-auto rounded-t-card-lg bg-white p-6 md:rounded-card-lg md:p-8"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-6">
          <h2 className="text-promo-s">{title}</h2>
          <button
            type="button"
            aria-label="Закрыть"
            onClick={onClose}
            className="-mt-1 -mr-1 flex size-9 shrink-0 items-center justify-center rounded-full text-[24px] leading-none text-ink-soft hover:bg-black/6"
          >
            ×
          </button>
        </div>
        {children}
        {footer}
      </div>
    </div>
  );
}
