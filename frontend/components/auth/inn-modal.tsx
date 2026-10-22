"use client";

import { Modal } from "@/components/ui/modal";
import { DemoNotice } from "@/components/ui/demo-notice";
import { ErrorBanner } from "@/components/ui/error-banner";
import { innCatalog } from "@/lib/demo-data";

const primary =
  "inline-flex h-12 items-center justify-center rounded-full bg-brand px-8 text-[18px] font-medium text-white transition-colors hover:bg-brand-hover disabled:opacity-60";

type Props = {
  inn: string | null;
  error: string;
  busy: boolean;
  onSelect: (inn: string) => void;
  onContinue: () => void;
  onClose: () => void;
};

export function InnModal({ inn, error, busy, onSelect, onContinue, onClose }: Props) {
  return (
    <Modal
      title="Подключение компании"
      onClose={onClose}
      footer={
        <div className="mt-6 flex items-center justify-between gap-4">
          <span className="text-[14px] text-ink-soft">
            {inn ? `Выбран ИНН ${inn}` : "Выберите компанию из списка"}
          </span>
          <button type="button" onClick={onContinue} disabled={!inn || busy} className={primary}>
            {busy ? "Проверяем…" : "Продолжить"}
          </button>
        </div>
      }
    >
      <p className="mt-3 text-[15px] leading-6 text-ink-soft">
        Нажмите на строку, чтобы выбрать компанию, — или скопируйте ИНН и вставьте его в свою
        заявку.
      </p>
      <DemoNotice className="mt-4" />
      <div className="mt-4 space-y-2">
        {innCatalog.map((row) => (
          <div
            key={row.inn}
            role="button"
            tabIndex={0}
            onClick={() => onSelect(row.inn)}
            onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && onSelect(row.inn)}
            className={`flex cursor-pointer items-center gap-3 rounded-2xl border px-4 py-3 transition-colors ${
              inn === row.inn ? "border-brand bg-plate-tomato/50" : "border-black/8 hover:bg-black/4"
            }`}
          >
            <span className="shrink-0 rounded-full bg-plate-pearl px-2 py-0.5 text-[12px] font-medium">
              {row.kind === "ooo" ? "ООО" : "ИП"}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[15px]">{row.company}</span>
              <span className="block text-[13px] text-ink-soft">
                <span className="font-mono">{row.inn}</span> · {row.status}
              </span>
            </span>
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                void navigator.clipboard.writeText(row.inn);
              }}
              className="shrink-0 text-[13px] text-brand hover:underline"
            >
              Скопировать
            </button>
          </div>
        ))}
      </div>
      <ErrorBanner message={error} className="mt-4" />
    </Modal>
  );
}
