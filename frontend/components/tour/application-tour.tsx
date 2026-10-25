"use client";

import Link from "next/link";
import { useState } from "react";
import type { Application } from "@/lib/types-application";
import { Button } from "@/components/ui/button";
import { Tour, type TourStep } from "./tour";

const WITH_OFFER = new Set(["approved", "counter_offer", "signing", "issued"]);
const CLOSED = new Set(["rejected", "cancelled", "expired"]);

const pipeline: TourStep = {
  target: "status",
  title: "Статус заявки",
  text: "Путь пайплайна: «Отправлен» → «Проверка данных» → «Внешние проверки» → «Скоринг» → «Рассмотрение сотрудника». Страница обновляется сама.",
};

const infoExample: TourStep = {
  title: "Пример: запрос уточнений",
  text: "Если данных не хватает, сотрудник оставляет сообщение — например, «Приложите счёт поставщика» — и открывает отдельные поля для правки. Ответьте, поправьте открытые поля и нажмите «Отправить заявку повторно»: заявка вернётся на проверку.",
};

const decision: TourStep = {
  title: "Решение и подпись",
  text: "Положительное решение — предложение с суммой, ставкой, сроком, платежом и PDF договора. Читаете, соглашаетесь, подписываете (в демо PIN сертификата 12345678); после подписи остаются подписанный PDF и JSON-артефакт. Бывает и отказ.",
};

const guide: TourStep = {
  title: "Инструкция",
  text: (
    <>
      Полную карту путей: типы кредитов, шаги мастера, статусы и формат CSV — в{" "}
      <Link href="/guide" className="text-brand hover:underline">
        инструкции
      </Link>
      .
    </>
  ),
};

function stepsFor(app: Application): TourStep[] {
  if (app.status === "draft")
    return [
      pipeline,
      {
        target: "main",
        title: "Мастер заявки",
        text: "Пять шагов: параметры, залог (если выбрали обеспечение), проект и денежный план для «Старта», финансы и отправка. Черновик сохраняется при переходах между шагами.",
      },
      infoExample,
      decision,
      guide,
    ];
  if (app.status === "info_requested")
    return [
      pipeline,
      {
        target: "main",
        title: "Запрос уточнений",
        text: "Банк запросил данные: видно сообщение сотрудника и открытые для правки поля. Ответьте, поправьте открытые поля и нажмите «Отправить заявку повторно» — дальше заявка снова пройдёт проверку.",
      },
      decision,
      guide,
    ];
  if (WITH_OFFER.has(app.status))
    return [
      pipeline,
      {
        target: "main",
        title: "Предложение и подпись",
        text: "Условия решения: сумма, ставка, срок, платёж и PDF договора. Прочитайте и подпишите — в демо PIN сертификата 12345678. После подписи доступны подписанный PDF и JSON-артефакт подписи.",
      },
      guide,
    ];
  if (CLOSED.has(app.status))
    return [
      pipeline,
      {
        target: "main",
        title: "Заявка закрыта",
        text: "Действий больше нет — статус финальный, детали уточняются в банке.",
      },
      guide,
    ];
  return [
    pipeline,
    {
      target: "main",
      title: "Рассмотрение",
      text: "Заявка у сотрудника: он смотрит проверки и скоринг, при залоге или «Старте» проверяет обеспечение и денежный план. Если чего-то не хватает — придёт запрос уточнений.",
    },
    infoExample,
    decision,
    guide,
  ];
}

export function ApplicationTour({ app }: { app: Application }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="ghost" onClick={() => setOpen(true)}>
        Показать туториал
      </Button>
      {open && <Tour steps={stepsFor(app)} onClose={() => setOpen(false)} />}
    </>
  );
}
