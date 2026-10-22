"use client";

import { Modal } from "@/components/ui/modal";
import { DemoNotice } from "@/components/ui/demo-notice";
import { ErrorBanner } from "@/components/ui/error-banner";
import type { Challenge } from "@/lib/api";

type Busy = "sign" | "connect" | null;

type Props = {
  challenge: Challenge;
  ownerName: string;
  pin: string;
  error: string;
  busy: Busy;
  onPin: (pin: string) => void;
  onSign: () => void;
  onClose: () => void;
};

const labels: Record<Exclude<Busy, null>, string> = {
  sign: "Подписываем…",
  connect: "Подключаем компанию…",
};

export function SignModal({ challenge, ownerName, pin, error, busy, onPin, onSign, onClose }: Props) {
  return (
    <Modal title="Электронная подпись" onClose={onClose}>
      <div className="mt-3 flex items-baseline justify-between gap-4">
        <p className="text-[16px] font-medium">{challenge.company.name}</p>
        <p className="shrink-0 font-mono text-[14px] text-ink-soft">ИНН {challenge.company.inn}</p>
      </div>

      <p className="mt-4 text-[14px] text-ink-soft">Заявление о подключении компании</p>
      <pre className="mt-1 max-h-44 overflow-auto rounded-2xl bg-plate-pearl p-4 font-sans text-[13px] leading-5 whitespace-pre-wrap">
        {challenge.data}
      </pre>
      <DemoNotice className="mt-4" />

      <p className="mt-3 text-[14px] text-ink-soft">
        Владелец сертификата:{" "}
        <span className="font-medium text-ink">{ownerName || "тестовый сертификат"}</span>
      </p>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!busy) onSign();
        }}
        className="mt-4"
      >
        <label className="block">
          <span className="text-[14px] text-ink-soft">PIN-код электронной подписи</span>
          <input
            type="password"
            inputMode="numeric"
            value={pin}
            onChange={(e) => onPin(e.target.value)}
            className="mt-1 w-full rounded-2xl border border-black/10 px-4 py-3 text-[16px] outline-none focus:border-brand"
            autoComplete="off"
          />
        </label>
        <p className="mt-2 text-[13px] text-ink-soft">Тестовый PIN-код: 12345678</p>

        <ErrorBanner message={error} className="mt-4" />

        <button
          type="submit"
          disabled={busy !== null || !pin}
          className="mt-6 inline-flex h-14 w-full items-center justify-center rounded-full bg-brand px-8 text-[18px] font-medium text-white transition-colors hover:bg-brand-hover disabled:opacity-60"
        >
          {busy ? labels[busy] : "Подписать и подключить"}
        </button>
      </form>
    </Modal>
  );
}
