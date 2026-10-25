"use client";

import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Tour, type TourStep } from "./tour";

const steps: TourStep[] = [
  {
    target: "company",
    title: "Компания",
    text: "Здесь данные подключённой компании: ИНН, статус из реестра и право подписи — их подтягивает подключение по ИНН.",
  },
  {
    target: "applications",
    title: "Заявки",
    text: "Кнопка «Новая заявка» открывает мастер из пяти шагов. Список показывает статус каждой заявки — по нему видно, где заявка в пути.",
  },
  {
    title: "Путь заявки",
    text: "Пайплайн: отправка → автоматические проверки и скоринг → сотрудник банка. Если данных не хватает — запрос уточнений: вы отвечаете, правите открытые поля и отправляете заявку повторно.",
  },
  {
    title: "Решение и подпись",
    text: "Положительное решение даёт предложение с условиями и PDF договора: читаете, подписываете (в демо PIN сертификата 12345678) — подписанный PDF и JSON подписи остаются у вас. Возможен и отказ.",
  },
  {
    title: "Дальше",
    text: (
      <>
        Откройте заявку — туториал продолжится на ней и покажет блоки вживую. Полная версия путей и
        статусов — в{" "}
        <Link href="/guide" className="text-brand hover:underline">
          инструкции
        </Link>
        .
      </>
    ),
  },
];

export function CabinetTour() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="ghost" className="mb-6" onClick={() => setOpen(true)}>
        Показать туториал
      </Button>
      {open && <Tour steps={steps} onClose={() => setOpen(false)} />}
    </>
  );
}
