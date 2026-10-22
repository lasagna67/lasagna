"use client";

import { useState } from "react";
import { ApiError } from "@/lib/api";
import { ErrorBanner } from "@/components/ui/error-banner";

type Props = {
  busy: boolean;
  onRegister: (email: string, password: string) => Promise<void>;
  onLoginExisting: (email: string, password: string) => Promise<void>;
  onBack: () => void;
};

const input =
  "w-full rounded-2xl border border-black/10 px-4 py-3 text-[16px] outline-none focus:border-brand";
const primary =
  "inline-flex h-12 items-center justify-center rounded-full bg-brand px-8 text-[18px] font-medium text-white transition-colors hover:bg-brand-hover disabled:opacity-60";

export function RegisterStep({ busy, onRegister, onLoginExisting, onBack }: Props) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState("");
  const [emailTaken, setEmailTaken] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setError("Некорректный email");
      return;
    }
    if (password.length < 8 || password.length > 72) {
      setError("Пароль должен быть от 8 до 72 символов");
      return;
    }
    try {
      await onRegister(email, password);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setEmailTaken(true);
        return;
      }
      setError(err instanceof ApiError ? err.message : "Неизвестная ошибка");
    }
  }

  return (
    <form onSubmit={submit} className="mt-8 space-y-4">
      <label className="block">
        <span className="text-[14px] text-ink-soft">Почта</span>
        <input
          type="email"
          value={email}
          onChange={(e) => {
            setEmail(e.target.value);
            setEmailTaken(false);
          }}
          placeholder="owner@example.com"
          className={`mt-1 ${input}`}
          autoComplete="email"
        />
      </label>
      <label className="block">
        <span className="text-[14px] text-ink-soft">Пароль</span>
        <span className="relative mt-1 block">
          <input
            type={showPassword ? "text" : "password"}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="От 8 до 72 символов"
            className={`${input} pr-20`}
            autoComplete="new-password"
          />
          <button
            type="button"
            onClick={() => setShowPassword((v) => !v)}
            className="absolute top-1/2 right-3 -translate-y-1/2 text-[14px] text-ink-soft hover:text-brand"
          >
            {showPassword ? "Скрыть" : "Показать"}
          </button>
        </span>
      </label>

      <ErrorBanner message={error} />

      {emailTaken && (
        <div className="rounded-2xl bg-plate-chardonnay px-4 py-3 text-[14px] leading-5">
          Пользователь с таким email уже зарегистрирован — войдите, чтобы продолжить подключение.
          <button
            type="button"
            onClick={() => onLoginExisting(email, password).catch((err) =>
              setError(err instanceof ApiError ? err.message : "Неизвестная ошибка"),
            )}
            className="ml-2 font-medium text-brand hover:underline"
          >
            Войти с этим email
          </button>
        </div>
      )}

      <div className="flex items-center gap-4 pt-2">
        <button type="submit" disabled={busy} className={primary}>
          {busy ? "Создаём аккаунт…" : "Зарегистрироваться"}
        </button>
        <button
          type="button"
          onClick={onBack}
          className="text-[16px] text-ink-soft hover:text-brand"
        >
          ← Вернуться к выбору сценария
        </button>
      </div>
    </form>
  );
}
