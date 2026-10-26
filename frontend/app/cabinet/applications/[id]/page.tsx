"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { Suspense, use, useCallback, useEffect, useState } from "react";
import { ApiError, api } from "@/lib/api";
import { appStatusLabel } from "@/lib/demo-data";
import { clearSession, getSession } from "@/lib/session";
import type { AppStatus, Application, Contract, InfoRequest, Offer } from "@/lib/types-application";
import { AppSummary } from "@/components/application/app-summary";
import { ApplyWizard } from "@/components/application/apply-wizard";
import { BankReviews } from "@/components/application/bank-reviews";
import { InfoPanel } from "@/components/application/info-panel";
import { OfferPanel } from "@/components/application/offer-panel";
import { StatusBar } from "@/components/application/status-bar";
import { ApplicationTour } from "@/components/tour/application-tour";
import { AppHeader } from "@/components/ui/app-header";
import { ErrorBanner } from "@/components/ui/error-banner";

const POLL = new Set<AppStatus>(["submitted", "validation", "enrichment", "scoring", "signing"]);
const WITH_OFFER = new Set<AppStatus>(["approved", "counter_offer", "signing", "issued"]);
const TERMINAL = new Set<AppStatus>(["rejected", "cancelled", "expired"]);
const WAITING = new Set<AppStatus>([
  "submitted",
  "validation",
  "enrichment",
  "scoring",
  "manual_review",
]);

type Data = {
  token: string;
  email?: string;
  app: Application;
  offer: Offer | null;
  contract: Contract | null;
  infoRequests: InfoRequest[];
  canSign: boolean;
};
type State =
  | { phase: "loading" }
  | { phase: "error"; message: string }
  | ({ phase: "ready" } & Data);

export default function ApplicationPage({ params }: { params: Promise<{ id: string }> }) {
  return (
    <Suspense fallback={<p className="mx-auto max-w-[1200px] px-4 py-10 text-ink-soft">Загружаем заявку…</p>}>
      <ApplicationDetail params={params} />
    </Suspense>
  );
}

function ApplicationDetail({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();
  const [state, setState] = useState<State>({ phase: "loading" });
  const [conflict, setConflict] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(
    async (quiet = false) => {
      const session = getSession();
      if (!session) {
        router.replace("/login");
        return;
      }
      const token = session.accessToken;
      if (!quiet) setState({ phase: "loading" });
      try {
        const app = await api.getApp(id, token);
        const withOffer = WITH_OFFER.has(app.status);
        const [offer, contract, infoRequests, canSign] = await Promise.all([
          withOffer ? api.offer(id, token).catch(() => null) : Promise.resolve(null),
          withOffer ? api.contract(id, token).catch(() => null) : Promise.resolve(null),
          app.status === "info_requested"
            ? api.infoRequests(id, token).catch(() => [] as InfoRequest[])
            : Promise.resolve([] as InfoRequest[]),
          withOffer
            ? api
                .company(app.company_id, token)
                .then((c) => c.membership.can_sign)
                .catch(() => false)
            : Promise.resolve(false),
        ]);
        setConflict(false);
        setState({ phase: "ready", token, email: session.email, app, offer, contract, infoRequests, canSign });
      } catch (e) {
        if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
          clearSession();
          router.replace("/login");
          return;
        }
        setState({ phase: "error", message: e instanceof ApiError ? e.message : "Неизвестная ошибка" });
      }
    },
    [id, router],
  );

  useEffect(() => {
    const timer = setTimeout(() => void load(), 0);
    return () => clearTimeout(timer);
  }, [load]);

  const status = state.phase === "ready" ? state.app.status : null;
  useEffect(() => {
    if (!status || !POLL.has(status)) return;
    const timer = setInterval(() => void load(true), 3000);
    return () => clearInterval(timer);
  }, [status, load]);

  async function logout() {
    const session = getSession();
    if (session) await api.logout(session.accessToken);
    clearSession();
    router.replace("/login");
  }

  function onUpdated(app: Application) {
    setState((prev) => (prev.phase === "ready" ? { ...prev, app } : prev));
  }

  function onConflict() {
    setConflict(true);
    void load(true);
  }

  async function refresh() {
    setRefreshing(true);
    try {
      await load(true);
    } finally {
      setRefreshing(false);
    }
  }

  return (
    <div className="min-h-dvh bg-white">
      <AppHeader
        chip="Бизнес"
        email={state.phase === "ready" ? state.email : undefined}
        onLogout={() => void logout()}
      />
      <main className="mx-auto w-full max-w-[1200px] px-4 pt-6 pb-20 md:px-8">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <Link href="/cabinet" className="text-[15px] text-ink-soft hover:text-brand">
            ← К кабинету
          </Link>
          {state.phase === "ready" && <ApplicationTour app={state.app} />}
        </div>

        {state.phase === "loading" && <p className="mt-8 text-ink-soft">Загружаем заявку…</p>}
        {state.phase === "error" && (
          <div className="mt-8">
            <ErrorBanner message={state.message} onRetry={() => void load()} />
          </div>
        )}

        {state.phase === "ready" && (
          <>
            <div className="mt-6" data-tour="status">
              <StatusBar
                status={state.app.status}
                number={state.app.number}
                submittedAt={state.app.submitted_at}
              />
            </div>
            {conflict && (
              <p className="mt-4 rounded-2xl bg-plate-chardonnay px-4 py-3 text-[14px]">
                Данные заявки изменились на сервере — обновили. Проверьте значения перед следующим
                сохранением.
              </p>
            )}

            <div className="mt-6" data-tour="main">
              {state.app.status === "draft" && (
                <ApplyWizard
                  app={state.app}
                  token={state.token}
                  onUpdated={onUpdated}
                  onConflict={onConflict}
                  onRefresh={refresh}
                />
              )}

              {state.app.status === "info_requested" && (
                <InfoPanel
                  app={state.app}
                  token={state.token}
                  requests={state.infoRequests}
                  onUpdated={onUpdated}
                  onConflict={onConflict}
                  onRefresh={refresh}
                />
              )}

              {WITH_OFFER.has(state.app.status) &&
                (state.offer ? (
                  <OfferPanel
                    app={state.app}
                    offer={state.offer}
                    contract={state.contract}
                    token={state.token}
                    canSign={state.canSign}
                    onSigned={refresh}
                    onConflict={onConflict}
                    onOfferChanged={refresh}
                  />
                ) : (
                  <p className="rounded-card-lg border border-black/8 p-6 text-ink-soft">
                    Предложение ещё не сформировано.{" "}
                    <button type="button" className="text-brand hover:underline" onClick={() => void refresh()}>
                      Обновить
                    </button>
                  </p>
                ))}

              {WAITING.has(state.app.status) && (
                <section className="rounded-card-lg border border-black/8 p-6">
                  <p className="text-[15px]">
                    {state.app.status === "manual_review"
                      ? "Заявка на проверке у сотрудника банка."
                      : "Заявка обрабатывается — обновим страницу автоматически."}
                  </p>
                  <button
                    type="button"
                    className="mt-3 text-[15px] text-brand hover:underline"
                    disabled={refreshing}
                    onClick={() => void refresh()}
                  >
                    {refreshing ? "Обновляем…" : "Обновить сейчас"}
                  </button>
                  <div className="mt-6">
                    <AppSummary app={state.app} />
                  </div>
                </section>
              )}

              {TERMINAL.has(state.app.status) && (
                <section className="rounded-card-lg border border-black/8 p-6">
                  <p className="text-[16px] font-medium">Статус: {appStatusLabel(state.app.status)}</p>
                  <p className="mt-2 text-[15px] text-ink-soft">
                    Заявка закрыта. Уточнить детали можно в банке.
                  </p>
                  <div className="mt-6">
                    <AppSummary app={state.app} />
                  </div>
                </section>
              )}
            </div>

            <BankReviews app={state.app} />
          </>
        )}
      </main>
    </div>
  );
}
