import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";

export function Hero() {
  return (
    <section className="pt-8 md:pt-16">
      <Container className="grid items-center gap-10 md:grid-cols-[1.1fr_0.9fr] md:gap-16">
        <div>
          <h1 className="text-promo-xl max-w-[15ch]">Кредит для бизнеса онлайн</h1>
          <p className="mt-6 max-w-[46ch] text-[18px] leading-6 text-ink-soft">
            Заявка за 10 минут, решение по данным из ЕГРЮЛ и кредитной истории — без визитов в
            банк.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Button href="#apply" size="l">
              Оставить заявку
            </Button>
            <Button href="#calculator" size="l" variant="ghost">
              Рассчитать платёж
            </Button>
          </div>
        </div>
        <div className="relative overflow-hidden rounded-card-lg bg-plate-chardonnay p-8 md:p-10">
          <svg aria-hidden viewBox="0 0 64 64" className="absolute -right-4 -top-4 size-24 opacity-60">
            <circle cx="32" cy="32" r="32" fill="#ef3124" />
          </svg>
          <p className="relative text-[16px] font-medium">Кредит для бизнеса</p>
          <div className="relative mt-5 max-w-[340px] rounded-card bg-white p-6 shadow-[0_4px_16px_rgba(0,0,0,0.06)]">
            <span className="inline-flex items-center gap-1.5 rounded-full bg-plate-menthol px-3 py-1 text-[14px] font-medium">
              <span aria-hidden>✓</span> Одобрено
            </span>
            <p className="mt-4 text-[32px] font-medium leading-9">3 000 000 ₽</p>
            <p className="mt-1 text-[14px] text-ink-soft">24 месяца · 15,9% годовых</p>
            <div className="mt-4 flex items-baseline justify-between border-t border-black/8 pt-4">
              <span className="text-[14px] text-ink-soft">Платёж</span>
              <span className="text-[18px] font-semibold">146 746 ₽/мес</span>
            </div>
            <span
              aria-hidden
              className="mt-5 inline-flex h-10 w-full items-center justify-center rounded-full bg-brand px-5 text-[16px] font-medium text-white"
            >
              Принять оффер
            </span>
          </div>
          <p className="relative mt-5 max-w-[36ch] text-[14px] leading-5 text-ink-soft">
            Пример оффера в кабинете. Точные условия — после проверки данных.
          </p>
        </div>
      </Container>
    </section>
  );
}
