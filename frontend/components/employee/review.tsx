"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { ApiError, api } from "@/lib/api";
import { dateRu } from "@/lib/demo-data";
import { clearSession, getSession } from "@/lib/session";
import type { ReviewCard } from "@/lib/types-underwriter";
import { StatusBar } from "@/components/application/status-bar";
import { AppHeader } from "@/components/ui/app-header";
import { ErrorBanner } from "@/components/ui/error-banner";
import { ActionPanel } from "./action-panel";
import { ReviewHistory } from "./review-history";
import { ReviewSections } from "./review-sections";

type Props = { id: string };

export function Review({ id }: Props) {
  const router = useRouter();
  const [card, setCard] = useState<ReviewCard | null>(null);
  const [token, setToken] = useState("");
  const [meId, setMeId] = useState("");
  const [email, setEmail] = useState<string | undefined>();
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);

  const load = useCallback(async () => {
    const session = getSession();
    if (!session) {
      router.replace("/login");
      return;
    }
    const tok = session.accessToken;
    setError("");
    try {
      const me = await api.me(tok);
      if (me.role !== "underwriter" && me.role !== "admin") {
        router.replace("/cabinet");
        return;
      }
      setCard(await api.uwGet(id, tok));
      setToken(tok);
      setMeId(me.id);
      setEmail(session.email);
      setConflict(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        clearSession();
        router.replace("/login");
        return;
      }
      setError(e instanceof ApiError ? e.message : "Неизвестная ошибка");
    }
  }, [id, router]);

  useEffect(() => {
    const timer = setTimeout(() => void load(), 0);
    return () => clearTimeout(timer);
  }, [load]);

  function onConflict() {
    setConflict(true);
    void load();
  }

  return (
    <div className="min-h-dvh bg-white">
      <AppHeader chip="Сотрудник" email={email} onLogout={() => router.replace("/login")} />
      <main className="mx-auto w-full max-w-[1200px] px-4 pt-6 pb-20 md:px-8">
        <Link href="/underwriter" className="text-[15px] text-ink-soft hover:text-brand">
          ← К очереди
        </Link>

        {!card && !error && <p className="mt-8 text-ink-soft">Загружаем карточку…</p>}
        {error && !card && (
          <div className="mt-8">
            <ErrorBanner message={error} onRetry={() => void load()} />
          </div>
        )}

        {card && (
          <>
            <div className="mt-6 flex flex-wrap items-center gap-x-4 gap-y-3">
              <StatusBar status={card.status} number={card.number} submittedAt={card.submitted_at} />
              <span className="rounded-full bg-plate-lagoon px-3 py-1 text-[14px]">
                {card.company.name}
              </span>
              <span className="rounded-full bg-plate-pearl px-3 py-1 text-[14px]">
                {card.assignee
                  ? card.assignee.id === meId
                    ? "на вас"
                    : `на ${card.assignee.display_name}`
                  : "свободна"}
              </span>
              <span className="text-[13px] text-ink-soft">
                {card.request.amount_kopecks != null &&
                  `${(card.request.amount_kopecks / 100).toLocaleString("ru-RU")} ₽ · `}
                обновлена {dateRu(card.updated_at)} · v{card.revision}
              </span>
            </div>

            {conflict && (
              <p className="mt-4 rounded-2xl bg-plate-chardonnay px-4 py-3 text-[14px]">
                Заявка изменилась на сервере — данные обновлены. Проверьте поля и повторите действие.
              </p>
            )}
            {error && (
              <div className="mt-4">
                <ErrorBanner message={error} onRetry={() => void load()} />
              </div>
            )}

            <div className="mt-6">
              <ActionPanel card={card} token={token} onActed={load} onConflict={onConflict} />
            </div>

            <ReviewSections card={card} />
            <ReviewHistory card={card} />
          </>
        )}
      </main>
    </div>
  );
}
