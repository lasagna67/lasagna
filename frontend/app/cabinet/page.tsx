"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ApiError, api, type Company, type Overview } from "@/lib/api";
import type { Application } from "@/lib/types-application";
import { businessStatusLabel, entityLabel, positionLabel } from "@/lib/demo-data";
import { clearSession, getSession, setSession } from "@/lib/session";
import { ConnectFlow } from "@/components/auth/connect-flow";
import { ApplicationsSection } from "@/components/cabinet/applications-section";
import { OverviewSection } from "@/components/cabinet/overview";
import { AppHeader } from "@/components/ui/app-header";
import { Button } from "@/components/ui/button";
import { DemoNotice } from "@/components/ui/demo-notice";
import { ErrorBanner } from "@/components/ui/error-banner";

type Ready = {
  token: string;
  email?: string;
  companies: Company[];
  activeId: string;
  overview: Overview | null;
  apps: Application[];
};

type State = { phase: "loading" } | { phase: "error"; message: string; token?: string } | ({ phase: "ready" } & Ready);

const statusPlates: Record<string, string> = {
  active: "bg-plate-menthol",
  reorganizing: "bg-plate-chardonnay",
  terminated: "bg-plate-tomato",
};

export default function CabinetPage() {
  const router = useRouter();
  const [state, setState] = useState<State>({ phase: "loading" });
  const [connectOpen, setConnectOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  const loadCompanyData = async (token: string, company: Company, email?: string) => {
    const [overview, apps] = await Promise.all([
      api.overview(company.id, token).catch(() => null),
      api.listApps(company.id, token).then((p) => p.items).catch(() => [] as Application[]),
    ]);
    setState({
      phase: "ready",
      token,
      email,
      companies: [company],
      activeId: company.id,
      overview,
      apps,
    });
  };

  const load = useCallback(
    async (showLoading = false) => {
      const session = getSession();
      if (!session) {
        router.replace("/login");
        return;
      }
      if (showLoading) setState({ phase: "loading" });
      const token = session.accessToken;
      try {
        const [me, companies] = await Promise.all([api.me(token), api.companies(token)]);
        setSession({ ...session, role: me.role });
        if (me.role === "underwriter" || me.role === "admin") {
          router.replace("/underwriter");
          return;
        }
        if (companies.length === 0) {
          setState({ phase: "ready", token, email: session.email, companies: [], activeId: "", overview: null, apps: [] });
          return;
        }
        await loadCompanyData(token, companies[0], session.email);
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          clearSession();
          router.replace("/login");
          return;
        }
        setState({ phase: "error", message: e instanceof ApiError ? e.message : "Неизвестная ошибка", token });
      }
    },
    [router],
  );

  useEffect(() => {
    const timer = setTimeout(() => void load(), 0);
    return () => clearTimeout(timer);
  }, [load]);

  async function logout() {
    const session = getSession();
    if (session) await api.logout(session.accessToken);
    clearSession();
    router.replace("/login");
  }

  async function createApp() {
    if (state.phase !== "ready" || !state.activeId) return;
    setCreating(true);
    try {
      const app = await api.createApp(state.activeId, state.token);
      router.push(`/cabinet/applications/${app.id}`);
    } catch (e) {
      setState({
        phase: "error",
        message: e instanceof ApiError ? e.message : "Неизвестная ошибка",
        token: state.token,
      });
    } finally {
      setCreating(false);
    }
  }

  async function uploadCsv(file: File) {
    if (state.phase !== "ready" || !state.activeId) return;
    try {
      await api.uploadCompanyCsv(state.activeId, file, state.token);
      const overview = await api.overview(state.activeId, state.token);
      setState({ ...state, overview });
    } catch (e) {
      setState({ ...state, phase: "error", message: e instanceof ApiError ? e.message : "Ошибка загрузки CSV" });
    }
  }

  const company = state.phase === "ready" && state.companies.length ? state.companies[0] : undefined;

  return (
    <div className="min-h-dvh bg-white">
      <AppHeader
        chip="Бизнес"
        email={state.phase === "ready" ? state.email : undefined}
        onLogout={() => void logout()}
      />

      <main className="mx-auto w-full max-w-[1200px] px-4 pt-8 pb-20 md:px-8">
        <DemoNotice className="mb-6" />

        {state.phase === "loading" && <p className="text-ink-soft">Загружаем кабинет…</p>}

        {state.phase === "error" && (
          <ErrorBanner
            message={state.message}
            onRetry={state.token ? () => void load(true) : undefined}
          />
        )}

        {state.phase === "ready" && company === undefined && (
          <section className="rounded-card-lg bg-plate-pearl p-8 md:p-10">
            <h1 className="text-promo-m">Компания не подключена</h1>
            <p className="mt-3 max-w-[52ch] text-[16px] leading-6 text-ink-soft">
              Аккаунт создан, но компания ещё не привязана к кабинету. Подключите её по ИНН:
              проверим данные в реестре и подпишем заявление электронной подписью.
            </p>
            <Button className="mt-6" size="l" onClick={() => setConnectOpen(true)}>
              Подключить компанию
            </Button>
          </section>
        )}

        {state.phase === "ready" && company && (
          <>
            <section>
              <div className="flex flex-wrap items-baseline gap-x-4 gap-y-3">
                <h1 className="text-promo-m">{company.name}</h1>
                <span className="rounded-full bg-plate-pearl px-3 py-1 font-mono text-[14px]">
                  ИНН {company.inn}
                </span>
                <span className="rounded-full bg-plate-lagoon px-3 py-1 text-[14px]">
                  {entityLabel(company.entity_type)}
                </span>
                <span
                  className={`rounded-full px-3 py-1 text-[14px] ${statusPlates[company.business_status] ?? "bg-plate-pearl"}`}
                >
                  {businessStatusLabel(company.business_status)}
                </span>
                <span className="rounded-full bg-plate-sonata px-3 py-1 text-[14px]">
                  {positionLabel(company.membership.position)}
                  {company.membership.can_sign && " · право подписи"}
                </span>
              </div>
              <p className="mt-3 text-[15px] text-ink-soft">
                {company.head
                  ? `Руководитель: ${company.head.full_name} (${company.head.position})`
                  : "Руководитель не указан"}
                {company.primary_okved && ` · ОКВЭД ${company.primary_okved}`}
              </p>
            </section>

            <label className="mt-4 inline-flex cursor-pointer items-center gap-2 text-[14px] text-ink-soft hover:text-brand">
              Загрузить финансовую отчётность (CSV)
              <input
                type="file"
                accept=".csv,text/csv"
                className="sr-only"
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (file) void uploadCsv(file);
                  e.target.value = "";
                }}
              />
            </label>

            {state.overview && <OverviewSection overview={state.overview} />}

            <ApplicationsSection
              items={state.apps}
              busy={creating}
              canCreate={Boolean(company.membership)}
              onCreate={() => void createApp()}
            />
          </>
        )}
      </main>

      {connectOpen && state.phase === "ready" && (
        <ConnectFlow
          token={state.token}
          onClose={() => setConnectOpen(false)}
          onConnected={() => {
            setConnectOpen(false);
            void load();
          }}
        />
      )}
    </div>
  );
}
