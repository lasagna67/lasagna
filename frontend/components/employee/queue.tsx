"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, api } from "@/lib/api";
import { appStatusLabel, dateRu, kopecks } from "@/lib/demo-data";
import { clearSession, getSession } from "@/lib/session";
import type { QueueItem } from "@/lib/types-underwriter";
import { AppHeader } from "@/components/ui/app-header";
import { ErrorBanner } from "@/components/ui/error-banner";

const STATUSES = [
  "manual_review",
  "info_requested",
  "submitted",
  "validation",
  "enrichment",
  "scoring",
  "approved",
  "counter_offer",
  "signing",
  "issued",
  "rejected",
  "cancelled",
  "expired",
];

const statusPlates: Record<string, string> = {
  manual_review: "bg-plate-sonata",
  info_requested: "bg-plate-chardonnay",
  approved: "bg-plate-menthol",
  counter_offer: "bg-plate-lagoon",
  rejected: "bg-plate-tomato",
  signing: "bg-plate-lagoon",
};

type Filter = { status: string; assignee: string };

export function Queue() {
  const router = useRouter();
  const [email, setEmail] = useState<string | undefined>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [items, setItems] = useState<QueueItem[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>({ status: "", assignee: "" });
  // глубокая пагинация: пока пользователь листает страницы — список не перестраиваем
  const [appended, setAppended] = useState(false);
  const appendedRef = useRef(false);

  const load = useCallback(
    async (f: Filter, opts?: { appendAfter?: string; quiet?: boolean }) => {
      const session = getSession();
      if (!session) {
        router.replace("/login");
        return;
      }
      const tok = session.accessToken;
      const { appendAfter, quiet = false } = opts ?? {};
      if (!appendAfter && !quiet) setLoading(true);
      if (!quiet) setError("");
      try {
        const me = await api.me(tok);
        if (me.role !== "underwriter" && me.role !== "admin") {
          router.replace("/cabinet");
          return;
        }
        const page = await api.uwQueue(
          { status: f.status, assignee: f.assignee, limit: 20, cursor: appendAfter },
          tok,
        );
        if (quiet && appendedRef.current) return;
        setEmail(session.email);
        setItems((prev) => (appendAfter ? [...prev, ...page.items] : page.items));
        setCursor(page.next_cursor);
        appendedRef.current = Boolean(appendAfter);
        setAppended(Boolean(appendAfter));
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          clearSession();
          router.replace("/login");
          return;
        }
        if (!quiet) setError(e instanceof ApiError ? e.message : "Неизвестная ошибка");
      } finally {
        if (!quiet) setLoading(false);
      }
    },
    [router],
  );

  useEffect(() => {
    const timer = setTimeout(() => void load(filter), 0);
    return () => clearTimeout(timer);
  }, [load, filter]);

  // тихое обновление первой страницы каждые 3 с — пока не открыта пагинация
  useEffect(() => {
    if (appended) return;
    const timer = setInterval(() => void load(filter, { quiet: true }), 3000);
    return () => clearInterval(timer);
  }, [load, filter, appended]);

  function select(name: keyof Filter, value: string) {
    setFilter({ ...filter, [name]: value });
  }

  const selectCls =
    "rounded-xl border border-black/10 bg-white px-3 py-2 text-[14px] outline-none focus:border-brand";

  return (
    <div className="min-h-dvh bg-white">
      <AppHeader chip="Сотрудник" email={email} onLogout={() => router.replace("/login")} />
      <main className="mx-auto w-full max-w-[1200px] px-4 pt-8 pb-20 md:px-8">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <h1 className="text-promo-m">Очередь заявок</h1>
          <div className="flex flex-wrap gap-3">
            <select
              value={filter.status}
              onChange={(e) => select("status", e.target.value)}
              className={selectCls}
              aria-label="Статус"
            >
              <option value="">Все статусы</option>
              {STATUSES.map((s) => (
                <option key={s} value={s}>
                  {appStatusLabel(s)}
                </option>
              ))}
            </select>
            <select
              value={filter.assignee}
              onChange={(e) => select("assignee", e.target.value)}
              className={selectCls}
              aria-label="Ответственный"
            >
              <option value="">Любой ответственный</option>
              <option value="me">Мои заявки</option>
              <option value="none">Свободные</option>
            </select>
          </div>
        </div>

        {error && (
          <div className="mt-6">
            <ErrorBanner message={error} onRetry={() => void load(filter)} />
          </div>
        )}
        {loading && <p className="mt-6 text-ink-soft">Загружаем очередь…</p>}
        {!loading && !error && items.length === 0 && (
          <p className="mt-6 rounded-card-lg bg-plate-pearl p-6 text-ink-soft">
            По фильтру заявок нет.
          </p>
        )}

        <ul className="mt-6 space-y-3">
          {items.map((item) => (
            <li key={item.id}>
              <Link
                href={`/underwriter/${item.id}`}
                className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-card-lg border border-black/8 p-4 transition-colors hover:border-brand/40 hover:bg-plate-pearl/60"
              >
                <span className="font-mono text-[14px] text-ink-soft">№{item.number}</span>
                <span
                  className={`rounded-full px-3 py-1 text-[13px] ${statusPlates[item.status] ?? "bg-plate-pearl"}`}
                >
                  {appStatusLabel(item.status)}
                </span>
                <span className="text-[15px] font-medium">{item.company.name}</span>
                <span className="text-[14px] text-ink-soft">
                  {item.request.amount_kopecks != null ? kopecks(item.request.amount_kopecks) : "—"}
                  {item.request.product_code ? ` · ${item.request.product_code}` : ""}
                </span>
                <span className="ml-auto text-[13px] text-ink-soft">
                  {item.assignee ? `на ${item.assignee.display_name}` : "свободна"} · обновлена{" "}
                  {dateRu(item.updated_at)}
                </span>
              </Link>
            </li>
          ))}
        </ul>

        {cursor && (
          <button
            type="button"
            className="mt-6 text-[15px] text-brand hover:underline"
            disabled={loading}
            onClick={() => void load(filter, { appendAfter: cursor })}
          >
            {loading ? "Загружаем…" : "Показать ещё"}
          </button>
        )}
        {!cursor && items.length > 0 && (
          <p className="mt-6 text-[13px] text-ink-soft">Все заявки показаны.</p>
        )}
      </main>
    </div>
  );
}
