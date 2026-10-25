"use client";

import { useEffect, useRef, useState } from "react";
import { ApiError, api, isRevisionConflict, newIdemKey } from "@/lib/api";
import type { Application, Contract, Offer } from "@/lib/types-application";
import { documentUrl } from "@/lib/pdf";
import { FieldErrors, errorFields, errorMessage, input, primary } from "./form-ui";

type Props = {
  app: Application;
  offer: Offer;
  contract: Contract | null;
  token: string;
  onSigned: () => Promise<void>;
  onConflict: () => void;
  onOfferChanged: () => Promise<void>;
  onClose: () => void;
};

export function SignDialog({ app, offer, contract, token, onSigned, onConflict, onOfferChanged, onClose }: Props) {
  const [pin, setPin] = useState("12345678");
  const [agreed, setAgreed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [pdfUrl, setPdfUrl] = useState<string | null>(null);
  const idemRef = useRef<string | null>(null);

  useEffect(() => {
    let url: string | null = null;
    let alive = true;
    if (contract) {
      documentUrl(contract.id, token)
        .then((u) => {
          if (alive) setPdfUrl((url = u));
          else URL.revokeObjectURL(u);
        })
        .catch(() => undefined);
    }
    return () => {
      alive = false;
      if (url) URL.revokeObjectURL(url);
    };
  }, [contract, token]);

  async function signOnce() {
    const ctx = await api.signingContext(
      app.id,
      { expected_revision: app.revision, offer_id: offer.id, document_id: contract!.id },
      token,
    );
    const signed = await api.esignSign(ctx.certificate_serial, pin, ctx.data);
    return api.signApp(
      app.id,
      { context_id: ctx.context_id, signature: signed.signature, terms_accepted: true },
      token,
      idemRef.current ?? (idemRef.current = newIdemKey()),
    );
  }

  async function sign() {
    setBusy(true);
    setError(null);
    setNotice("");
    for (let attempt = 0; attempt < 2; attempt += 1) {
      try {
        await signOnce();
        await onSigned();
        onClose();
        return;
      } catch (e) {
        if (e instanceof ApiError && e.code === "SIGNING_CONTEXT_EXPIRED" && attempt === 0) {
          setNotice("Контекст подписи истёк — пробуем ещё раз.");
          continue;
        }
        setError(e);
        if (isRevisionConflict(e)) onConflict();
        if (e instanceof ApiError && e.code === "OFFER_CHANGED") {
          setNotice("Предложение изменилось — обновляем данные.");
          await onOfferChanged();
        }
        break;
      }
    }
    setBusy(false);
  }

  if (!contract) return null;
  const fieldErrors = errorFields(error);

  return (
    <div className="fixed inset-0 z-100 flex items-end justify-center bg-black/40 p-4 md:items-center" onClick={busy ? undefined : onClose}>
      <div
        className="max-h-[92vh] w-full max-w-[720px] overflow-y-auto rounded-t-card-lg bg-white p-6 md:rounded-card-lg md:p-8"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-6">
          <div>
            <h2 className="text-promo-s">Подписание договора</h2>
            <p className="mt-1 text-[14px] text-ink-soft">
              {offer.product_name} · номер договора {contract.version}
            </p>
          </div>
          <button
            type="button"
            aria-label="Закрыть"
            disabled={busy}
            onClick={onClose}
            className="-mt-1 -mr-1 flex size-9 shrink-0 items-center justify-center rounded-full text-[24px] leading-none text-ink-soft hover:bg-black/6"
          >
            ×
          </button>
        </div>

        {pdfUrl && (
          <iframe
            title="Договор"
            src={pdfUrl}
            className="mt-4 h-[300px] w-full rounded-2xl border border-black/10 bg-plate-pearl"
          />
        )}

        <label className="mt-4 flex items-start gap-3 text-[15px] leading-6">
          <input
            type="checkbox"
            checked={agreed}
            onChange={(e) => setAgreed(e.target.checked)}
            className="mt-1 size-4 accent-[#ef3124]"
          />
          <span>
            Согласен с условиями договора: {offer.product_name}, сумма{" "}
            {(offer.amount_kopecks / 100).toLocaleString("ru-RU")} ₽, ставка{" "}
            {(offer.annual_rate_bps / 100).toLocaleString("ru-RU")}%.
          </span>
        </label>

        <label className="mt-4 block">
          <span className="text-[14px] text-ink-soft">PIN-код сертификата</span>
          <input
            type="password"
            value={pin}
            onChange={(e) => setPin(e.target.value)}
            className={`${input} mt-1`}
            autoComplete="off"
          />
        </label>

        {notice && <p className="mt-3 text-[14px] text-ink-soft">{notice}</p>}
        <FieldErrors errors={fieldErrors} />
        {error != null && fieldErrors.length === 0 && (
          <p className="mt-3 text-[14px] text-brand">{errorMessage(error)}</p>
        )}

        <div className="mt-6 flex items-center gap-4">
          <button
            type="button"
            className={primary}
            disabled={!agreed || busy || !pin}
            onClick={() => void sign()}
          >
            {busy ? "Подписываем…" : "Подписать"}
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={onClose}
            className="text-[15px] text-ink-soft hover:text-brand"
          >
            Отмена
          </button>
        </div>
      </div>
    </div>
  );
}
