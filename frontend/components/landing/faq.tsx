import { Container } from "@/components/ui/container";

const questions = [
  {
    q: "Кто может получить кредит?",
    a: "ИП и ООО с регистрацией в России и действующим бизнесом от 6 месяцев. Решение зависит от данных компании в ЕГРЮЛ или ЕГРИП, кредитной истории и финансовых показателей из анкеты.",
  },
  {
    q: "Какие документы нужны?",
    a: "ИНН и паспорт руководителя или ИП. Дополнительно может понадобиться бухгалтерская отчётность или выписка по счёту, а для продуктов с обеспечением — документы на залог.",
  },
  {
    q: "Как принимается решение?",
    a: "Мы запрашиваем выписку из реестра и кредитную историю из БКИ, проверяем данные анкеты и рассчитываем оценку риска. Итог — оффер с суммой, сроком, ставкой и платежом либо отказ с понятными причинами.",
  },
  {
    q: "Обязывает ли заявка что-то?",
    a: "Нет. Заявка ничего не стоит и ни к чему не обязывает: вы можете не принимать предложение или подать заявку в другой раз.",
  },
];

export function Faq() {
  return (
    <section id="faq" className="scroll-mt-24">
      <Container>
        <h2 className="text-promo-m">Вопросы</h2>
        <div className="mt-8 border-t border-black/8">
          {questions.map(({ q, a }) => (
            <details key={q} className="group border-b border-black/8">
              <summary className="flex cursor-pointer list-none items-center justify-between gap-6 py-5 text-[18px] font-medium [&::-webkit-details-marker]:hidden">
                {q}
                <span aria-hidden className="text-[24px] leading-none text-brand transition-transform group-open:rotate-45">
                  +
                </span>
              </summary>
              <p className="max-w-[70ch] pb-5 text-[16px] leading-6 text-ink-soft">{a}</p>
            </details>
          ))}
        </div>
      </Container>
    </section>
  );
}
