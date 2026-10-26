"use client";

import { useEffect, useState } from "react";
import { ApiError, api, isRevisionConflict } from "@/lib/api";
import { dateRu } from "@/lib/demo-data";
import type { Application, Contract, Offer, Signature } from "@/lib/types-application";
import { downloadSignature, openDocument } from "@/lib/pdf";
import { SignDialog } from "./sign-dialog";
import { errorMessage, primary, secondary } from "./form-ui";

type Props = {
  app: Application;
  offer: Offer;
  contract: Contract | null;
  token: string;
  canSign: boolean;
  onSigned: () => Promise<void>;
  onConflict: () => void;
  onOfferChanged: () => Promise<void>;
};

const termLabel = (offer: Offer) =>
  offer.term_months == null
    ? `Овердрафт: лимит ${offer.agreement_term_months} мес${
        offer.max_tranche_days ? `, транш до ${offer.max_tranche_days} дн.` : ""
      }`
    : `${offer.term_months} мес.`;

const paymentLabel = (offer: Offer) =>
  offer.monthly_payment_kopecks == null
    ? "Формируется по траншам"
    : `${(offer.monthly_payment_kopecks / 100).toLocaleString("ru-RU")} ₽/мес.`;

export function OfferPanel({ app, offer, contract, token, canSign: canSignProp, onSigned, onConflict, onOfferChanged }: Props) {
  const [signing, setSigning] = useState(false);
  const [declining, setDeclining] = useState(false);
  const [signature, setSignature] = useState<Signature | null>(null);
  const [error, setError] = useState<unknown>(null);

  async function loadSignature() {
    try {
      setSignature(await api.signature(app.id, token));
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) setSignature(null);
      else setError(e);
    }
  }

  useEffect(() => {
    const timer = setTimeout(() => void loadSignature(), 0);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [app.id, app.status]);

  const signed = Boolean(signature);
  const canSign = !signed && canSignProp;
  const canDecline =
    !signed && (app.status === "approved" || app.status === "counter_offer");

  async function decline() {
    if (!confirm("Отказаться от предложения? Заявка будет закрыта.")) return;
    setDeclining(true);
    setError(null);
    try {
      await api.declineApp(app.id, app.revision, token);
      await onOfferChanged();
    } catch (e) {
      setError(e);
      if (isRevisionConflict(e)) onConflict();
    } finally {
      setDeclining(false);
    }
  }

  const rows: { label: string; value: string }[] = [
    { label: "Продукт", value: offer.product_name },
    { label: "Сумма", value: `${(offer.amount_kopecks / 100).toLocaleString("ru-RU")} ₽` },
    { label: "Ставка", value: `${(offer.annual_rate_bps / 100).toLocaleString("ru-RU")}%` },
    { label: "Срок", value: termLabel(offer) },
    { label: "Платёж", value: paymentLabel(offer) },
    { label: "Предложение от", value: dateRu(offer.created_at) },
    { label: "Предложение действительно до", value: dateRu(offer.valid_until) },
  ];

  async function openContract() {
    setError(null);
    try {
      if (contract) await openDocument(contract.id, token);
    } catch (e) {
      setError(e);
    }
  }

  async function openSignedDoc() {
    setError(null);
    try {
      if (signature?.signed_document_id) await openDocument(signature.signed_document_id, token);
    } catch (e) {
      setError(e);
    }
  }

  async function handleSigned() {
    await onSigned();
    await loadSignature();
    setSigning(false);
  }

  return (
    <section className="mt-8 rounded-card-lg border border-black/8 p-6 md:p-8">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-promo-m">Предложение банка</h2>
        {offer.outcome === "counter_offer" && (
          <span className="rounded-full bg-plate-chardonnay px-3 py-1 text-[13px]">
            Контрпредложение
          </span>
        )}
      </div>

      <dl className="mt-5 grid gap-x-8 gap-y-3 sm:grid-cols-2">
        {rows.map((r) => (
          <div
            key={r.label}
            className="flex items-baseline justify-between gap-4 border-b border-black/6 pb-2"
          >
            <dt className="text-[14px] text-ink-soft">{r.label}</dt>
            <dd className="text-right text-[15px] font-medium">{r.value}</dd>
          </div>
        ))}
      </dl>

      {(offer.changed_fields.length > 0 || offer.reasons.length > 0) && (
        <div className="mt-5 rounded-2xl bg-plate-sonata/70 px-4 py-3 text-[14px] leading-6">
          {offer.changed_fields.length > 0 && (
            <p>
              <span className="font-medium">Изменённые условия:</span>{" "}
              {offer.changed_fields.join(", ")}
            </p>
          )}
          {offer.reasons.length > 0 && (
            <ul className="mt-1 list-disc pl-5 text-ink-soft">
              {offer.reasons.map((r, i) => (
                <li key={i}>{r}</li>
              ))}
            </ul>
          )}
        </div>
      )}

      {error != null && (
        <p className="mt-4 text-[14px] text-brand">
          {errorMessage(error) || "Не удалось открыть документ, попробуйте позже."}
        </p>
      )}

      <div className="mt-6 flex flex-wrap items-center gap-3">
        <button type="button" className={secondary} onClick={() => void openContract()}>
          Открыть договор (PDF)
        </button>
        {signed && (
          <>
            <button type="button" className={secondary} onClick={() => void openSignedDoc()}>
              Открыть подписанный PDF
            </button>
            <button
              type="button"
              className={secondary}
              onClick={() => void downloadSignature(app.id, token)}
            >
              Скачать подпись (JSON)
            </button>
          </>
        )}
        {canSign && (
          <button type="button" className={primary} onClick={() => setSigning(true)}>
            Подписать договор
          </button>
        )}
        {canDecline && (
          <button
            type="button"
            className={secondary}
            disabled={declining}
            onClick={() => void decline()}
          >
            {declining ? "Отказываемся…" : "Отказаться от предложения"}
          </button>
        )}
      </div>

      {signed && signature && (
        <p className="mt-4 text-[14px] text-ink-soft">
          Договор подписан {dateRu(signature.signed_at)} ·{" "}
          {signature.certificate.owner_full_name}. Выдача оформляется банком.
        </p>
      )}

      {signing && contract && (
        <SignDialog
          app={app}
          offer={offer}
          contract={contract}
          token={token}
          onSigned={handleSigned}
          onConflict={onConflict}
          onOfferChanged={onOfferChanged}
          onClose={() => setSigning(false)}
        />
      )}
      {signing && !contract && (
        <p className="mt-4 text-[14px] text-brand">
          Договор не загружен — обновите страницу и повторите попытку.
        </p>
      )}
    </section>
  );
}
