import type { ReactNode } from "react";
import { Container } from "@/components/ui/container";

function CheckItem({ children }: { children: ReactNode }) {
  return (
    <li className="flex gap-3">
      <span aria-hidden className="mt-0.5 font-semibold text-brand">
        ✓
      </span>
      <span>{children}</span>
    </li>
  );
}

export function Requirements() {
  return (
    <section id="requirements" className="scroll-mt-24">
      <Container>
        <h2 className="text-promo-m">Кто может подать заявку</h2>
        <div className="mt-8 grid gap-4 md:grid-cols-2">
          <div className="rounded-card bg-plate-chardonnay p-8">
            <h3 className="text-[18px] font-semibold">Требования к бизнесу</h3>
            <ul className="mt-5 flex flex-col gap-3 text-[16px] leading-6">
              <CheckItem>ИП или ООО, зарегистрированные в России</CheckItem>
              <CheckItem>Действующий бизнес от 6 месяцев</CheckItem>
              <CheckItem>Нет текущих просрочек по кредитам</CheckItem>
              <CheckItem>Вы — владелец или сотрудник с полномочиями подавать заявки</CheckItem>
            </ul>
          </div>
          <div className="rounded-card bg-plate-pearl p-8">
            <h3 className="text-[18px] font-semibold">Что понадобится</h3>
            <ul className="mt-5 flex flex-col gap-3 text-[16px] leading-6">
              <CheckItem>ИНН компании — реквизиты подтянутся из ЕГРЮЛ или ЕГРИП</CheckItem>
              <CheckItem>Паспорт руководителя или ИП</CheckItem>
              <CheckItem>Данные о выручке и расходах за последние полгода</CheckItem>
              <CheckItem>Документы на залог — если выберете продукт с обеспечением</CheckItem>
            </ul>
          </div>
        </div>
      </Container>
    </section>
  );
}
