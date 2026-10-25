"use client";

import { useState } from "react";
import { ApiError } from "@/lib/api";
import { ErrorBanner } from "@/components/ui/error-banner";

type Props = {
  busy: boolean;
  onLogin: (email: string, password: string) => Promise<void>;
  onBack: () => void;
};

const input =
  "w-full rounded-2xl border border-black/10 px-4 py-3 text-[16px] outline-none focus:border-brand";
const primary =
  "inline-flex h-12 items-center justify-center rounded-full bg-brand px-8 text-[18px] font-medium text-white transition-colors hover:bg-brand-hover disabled:opacity-60";

export function EmployeeStep({ busy, onLogin, onBack }: Props) {
  const [email, setEmail] = useState("underwriter@lasagna.demo");
  const [password, setPassword] = useState("DemoUnderwriter2026!");
  const [error, setError] = useState("");

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        setError("");
        onLogin(email, password).catch((err) =>
          setError(err instanceof ApiError ? err.message : "Неизвестная ошибка"),
        );
      }}
      className="mt-8 space-y-4"
    >
      <label className="block">
        <span className="text-[14px] text-ink-soft">Почта сотрудника</span>
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          className={`mt-1 ${input}`}
          autoComplete="email"
        />
      </label>
      <label className="block">
        <span className="text-[14px] text-ink-soft">Пароль</span>
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className={`mt-1 ${input}`}
          autoComplete="current-password"
        />
      </label>

      <p className="rounded-2xl bg-plate-sonata px-4 py-3 text-[14px] leading-5 text-ink-soft">
        Вход в кабинет сотрудника банка. Демо-аккаунт подставлен — можно войти сразу.
      </p>

      <ErrorBanner message={error} />

      <div className="flex items-center gap-4 pt-2">
        <button type="submit" disabled={busy} className={primary}>
          {busy ? "Входим…" : "Войти в кабинет банка"}
        </button>
        <button
          type="button"
          onClick={onBack}
          className="text-[16px] text-ink-soft hover:text-brand"
        >
          ← Назад
        </button>
      </div>
    </form>
  );
}
