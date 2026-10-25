"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ApiError, api } from "@/lib/api";
import { getSession } from "@/lib/session";
import { Button } from "@/components/ui/button";
import { AnswerPreview, SignPreview } from "./previews";
import { Tour, type TourStep } from "./tour";

const DEMO_REQUEST = {
  purpose: "working_capital",
  purpose_description: "Демо-заявка из туториала",
  amount_kopecks: 300_000_000,
  need_period: { unit: "months", value: 24 },
  product_code: "working_capital",
  term_months: 24,
  collateral_mode: "none",
};

export function CabinetTour() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function createDemo() {
    setBusy(true);
    setError("");
    try {
      const session = getSession();
      if (!session) {
        router.replace("/login");
        return;
      }
      const token = session.accessToken;
      const companies = await api.companies(token);
      if (!companies.length) {
        setError("В кабинете нет подключённой компании.");
        return;
      }
      const app = await api.createApp(companies[0].id, token);
      const patched = await api.patchApp(
        app.id,
        { expected_revision: app.revision, request: DEMO_REQUEST },
        token,
      );
      await api
        .fetchFinancials(app.id, { expected_revision: patched.revision }, token)
        .catch(() => undefined);
      window.localStorage.setItem("lasagna.tour", "application:1");
      router.push(`/cabinet/applications/${app.id}`);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Не удалось создать заявку");
    } finally {
      setBusy(false);
    }
  }

  const steps: TourStep[] = [
    {
      target: "company",
      title: "Компания",
      text: "Здесь данные подключённой компании: ИНН, статус из реестра и право подписи — их подтягивает подключение по ИНН.",
    },
    {
      target: "applications",
      title: "Заявки",
      text: "Кнопка «Новая заявка» открывает мастер из пяти шагов. Список показывает статус каждой заявки — по нему видно, где заявка в пути: отправка, проверки, решение.",
    },
    {
      title: "Создадим заявку на тестовых данных",
      text: (
        <>
          <p>
            Нажмите — создадим черновик с готовыми параметрами (оборотные средства, 3 000 000 ₽ на
            24 месяца) и откроем его. Заявка сразу уйдёт в пайплайн: проверки → сотрудник банка.
          </p>
          <Button className="mt-3" size="m" disabled={busy} onClick={() => void createDemo()}>
            {busy ? "Создаём…" : "Создать заявку"}
          </Button>
          {error && <p className="mt-2 text-brand">{error}</p>}
        </>
      ),
    },
    {
      title: "Если банк запросит уточнения",
      text: (
        <>
          <p>
            Сотрудник оставляет сообщение и открывает отдельные поля. Ответьте, поправьте открытые
            поля и нажмите «Отправить заявку повторно» — заявка вернётся на проверку.
          </p>
          <AnswerPreview />
        </>
      ),
    },
    {
      title: "Решение и подпись",
      text: (
        <>
          <p>
            Положительное решение даёт предложение: условия, PDF договора, подпись. Подписанный PDF
            и JSON-артефакт остаются у вас.
          </p>
          <SignPreview />
        </>
      ),
    },
    {
      title: "Инструкция",
      text: (
        <>
          Путь от входа до подписи целиком — в{" "}
          <Link href="/guide" className="text-brand hover:underline">
            инструкции
          </Link>
          : типы кредитов, шаги мастера, статусы и формат CSV.
        </>
      ),
    },
  ];

  return (
    <>
      <Button variant="ghost" className="mb-6" onClick={() => setOpen(true)}>
        Показать туториал
      </Button>
      {open && <Tour steps={steps} onClose={() => setOpen(false)} />}
    </>
  );
}
