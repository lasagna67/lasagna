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
        <div className="rounded-card-lg bg-plate-chardonnay p-8 md:p-10">
          <p className="text-[16px] font-medium">Кредит для бизнеса</p>
          <p className="mt-4 text-promo-l">от 15,9%</p>
          <p className="text-[16px]">годовых</p>
          <dl className="mt-8 grid grid-cols-2 gap-6 border-t border-black/8 pt-6">
            <div>
              <dt className="text-[14px] text-ink-soft">Сумма</dt>
              <dd className="mt-1 text-[24px] font-medium">до 30 млн ₽</dd>
            </div>
            <div>
              <dt className="text-[14px] text-ink-soft">Срок</dt>
              <dd className="mt-1 text-[24px] font-medium">до 5 лет</dd>
            </div>
          </dl>
        </div>
      </Container>
    </section>
  );
}
