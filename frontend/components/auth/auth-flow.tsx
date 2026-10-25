"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { ApiError, api } from "@/lib/api";
import { DEMO_PASSWORD, scenarioEmail, type BusinessKind } from "@/lib/demo-data";
import { homeForRole, setSession } from "@/lib/session";
import { DemoNotice } from "@/components/ui/demo-notice";
import { Logo } from "@/components/ui/logo";
import { ConnectFlow } from "./connect-flow";
import { EmployeeStep } from "./employee-step";
import { RegisterStep } from "./register-step";
import { ScenarioStep } from "./scenario-step";

type Step = "role" | "scenario" | "register" | "employee";

const roles = [
  { key: "banker", title: "Банкир", caption: "Сотрудник банка", plate: "bg-plate-pearl hover:bg-[#dfe3e3]" },
  { key: "ip", title: "ИП", caption: "Индивидуальный предприниматель", plate: "bg-plate-chardonnay hover:bg-[#f7c384]" },
  { key: "ooo", title: "ООО", caption: "Общество с ограниченной ответственностью", plate: "bg-plate-lagoon hover:bg-[#8ee8eb]" },
] as const;

function errorText(e: unknown): string {
  return e instanceof ApiError ? e.message : "Неизвестная ошибка";
}

export function AuthFlow() {
  const router = useRouter();
  const [step, setStep] = useState<Step>("role");
  const [kind, setKind] = useState<BusinessKind>("ooo");
  const [busyKey, setBusyKey] = useState<string | null>(null);
  const [registerBusy, setRegisterBusy] = useState(false);
  const [error, setError] = useState("");
  const [connectToken, setConnectToken] = useState<string | null>(null);
  const retryRef = useRef<(() => void) | null>(null);

  async function login(email: string, password: string) {
    const auth = await api.login(email, password);
    setSession({
      accessToken: auth.access_token,
      email,
      role: auth.user.role,
      expiresAt: auth.expires_at,
    });
    return auth;
  }

  async function pickScenario(key: string) {
    setBusyKey(key);
    setError("");
    retryRef.current = () => void pickScenario(key);
    try {
      const auth = await login(scenarioEmail(kind, key), DEMO_PASSWORD);
      router.push(homeForRole(auth.user.role));
    } catch (e) {
      setError(errorText(e));
      setBusyKey(null);
    }
  }

  async function register(email: string, password: string) {
    setRegisterBusy(true);
    try {
      await api.createUser(email, password);
      setConnectToken((await login(email, password)).access_token);
    } finally {
      setRegisterBusy(false);
    }
  }

  async function loginExisting(email: string, password: string) {
    setRegisterBusy(true);
    try {
      const auth = await login(email, password);
      const companies = await api.companies(auth.access_token).catch(() => null);
      if (companies?.length === 0) setConnectToken(auth.access_token);
      else router.push(homeForRole(auth.user.role));
    } finally {
      setRegisterBusy(false);
    }
  }

  async function loginEmployee(email: string, password: string) {
    setRegisterBusy(true);
    try {
      const auth = await login(email, password);
      router.push(homeForRole(auth.user.role));
    } finally {
      setRegisterBusy(false);
    }
  }

  const titles: Record<Step, { title: string; caption: string }> = {
    role: { title: "Кто вы?", caption: "Выберите тип аккаунта — от него зависит интерфейс кабинета." },
    scenario: {
      title: kind === "ooo" ? "Сценарии для ООО" : "Сценарии для ИП",
      caption: "Выберите готовый сценарий — войдём в тестовый аккаунт с нужными данными.",
    },
    register: {
      title: "Регистрация",
      caption: "Создайте аккаунт и подключите компанию по ИНН из мокового реестра.",
    },
    employee: {
      title: "Кабинет сотрудника",
      caption: "Вход для сотрудников банка: очередь заявок, проверки и решения.",
    },
  };

  return (
    <div className="min-h-dvh bg-white">
      <header className="border-b border-black/4">
        <div className="mx-auto flex h-16 w-full max-w-[1200px] items-center px-4 md:h-20 md:px-8">
          <Link href="/" className="flex items-center gap-2.5">
            <Logo />
            <span className="rounded-full bg-plate-tomato px-2 py-0.5 text-[12px] font-medium">
              Бизнес
            </span>
          </Link>
        </div>
      </header>

      <main className="mx-auto w-full max-w-[720px] px-4 pt-8 pb-20 md:px-6">
        <DemoNotice className="mb-8" />
        <h1 className="text-promo-l">{titles[step].title}</h1>
        <p className="mt-3 text-[16px] leading-6 text-ink-soft">{titles[step].caption}</p>

        {step === "role" && (
          <div className="mt-8 grid gap-4 sm:grid-cols-3">
            {roles.map((role) => (
              <button
                key={role.key}
                type="button"
                onClick={() =>
                  role.key === "banker"
                    ? setStep("employee")
                    : (setKind(role.key as BusinessKind), setStep("scenario"))
                }
                className={`rounded-card-lg p-6 text-left transition-colors ${role.plate}`}
              >
                <span className="block text-promo-s">{role.title}</span>
                <span className="mt-2 block text-[14px] leading-5 text-ink-soft">
                  {role.caption}
                </span>
              </button>
            ))}
          </div>
        )}

        {step === "scenario" && (
          <ScenarioStep
            kind={kind}
            busyKey={busyKey}
            error={error}
            onPick={(key) => void pickScenario(key)}
            onRetry={busyKey ? null : retryRef.current}
            onRegister={() => setStep("register")}
            onBack={() => setStep("role")}
          />
        )}

        {step === "register" && (
          <RegisterStep
            busy={registerBusy}
            onRegister={register}
            onLoginExisting={loginExisting}
            onBack={() => setStep("scenario")}
          />
        )}

        {step === "employee" && (
          <EmployeeStep
            busy={registerBusy}
            onLogin={loginEmployee}
            onBack={() => setStep("role")}
          />
        )}
      </main>

      {connectToken && (
        <ConnectFlow
          token={connectToken}
          onClose={() => setConnectToken(null)}
          onConnected={() => router.push("/cabinet")}
        />
      )}
    </div>
  );
}
