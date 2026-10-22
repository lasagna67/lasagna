"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ApiError, api, type Company, type Overview } from "@/lib/api";
import { businessStatusLabel, entityLabel, positionLabel } from "@/lib/demo-data";
import { clearSession, getSession, setSession } from "@/lib/session";
import { ConnectFlow } from "@/components/auth/connect-flow";
import { OverviewSection } from "@/components/cabinet/overview";
import { Button } from "@/components/ui/button";
import { DemoNotice } from "@/components/ui/demo-notice";
import { ErrorBanner } from "@/components/ui/error-banner";
import { Logo } from "@/components/ui/logo";

type State =
  | { phase: "loading" }
  | { phase: "error"; message: string; token: string }
  | { phase: "ready"; token: string; email?: string; companies: Company[]; overview: Overview | null };

const statusPlates: Record<string, string> = {
  active: "bg-plate-menthol",
  reorganizing: "bg-plate-chardonnay",
  terminated: "bg-plate-tomato",
};

export default function CabinetPage() {
  const router = useRouter();
  const [state, setState] = useState<State>({ phase: "loading" });
  const [connectOpen, setConnectOpen] = useState(false);

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
        const overview = companies.length ? await api.overview(companies[0].id, token) : null;
        setState({ phase: "ready", token, email: session.email, companies, overview });
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          clearSession();
          router.replace("/login");
          return;
        }
        setState({
          phase: "error",
          message: e instanceof ApiError ? e.message : "Неизвестная ошибка",
          token,
        });
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

  const company = state.phase === "ready" ? state.companies[0] : undefined;

  return (
    <div className="min-h-dvh bg-white">
      <header className="border-b border-black/4">
        <div className="mx-auto flex h-16 w-full max-w-[1200px] items-center gap-6 px-4 md:h-20 md:px-8">
          <Link href="/" className="flex items-center gap-2.5">
            <Logo />
            <span className="rounded-full bg-plate-tomato px-2 py-0.5 text-[12px] font-medium">
              Бизнес
            </span>
          </Link>
          {state.phase !== "loading" && (
            <div className="ml-auto flex items-center gap-4">
              {"email" in state && state.email && (
                <span className="hidden text-[14px] text-ink-soft sm:block">{state.email}</span>
              )}
              <Button variant="ghost" onClick={() => void logout()}>
                Выйти
              </Button>
            </div>
          )}
        </div>
      </header>

      <main className="mx-auto w-full max-w-[1200px] px-4 pt-8 pb-20 md:px-8">
        <DemoNotice className="mb-6" />

        {state.phase === "loading" && <p className="text-ink-soft">Загружаем кабинет…</p>}

        {state.phase === "error" && (
          <ErrorBanner message={state.message} onRetry={() => void load(true)} />
        )}

        {state.phase === "ready" &&
          (company === undefined ? (
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
          ) : (
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

              {state.overview && <OverviewSection overview={state.overview} />}
            </>
          ))}
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
