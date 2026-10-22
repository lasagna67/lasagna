import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";

export function Cta() {
  return (
    <section id="apply" className="scroll-mt-24">
      <Container>
        <div className="flex flex-col gap-8 rounded-card-lg bg-plate-tomato p-10 md:flex-row md:items-center md:justify-between md:p-14">
          <div>
            <h2 className="text-promo-m">Подайте заявку прямо сейчас</h2>
            <p className="mt-3 max-w-[46ch] text-[16px] leading-6">
              Понадобятся ИНН и паспорт — заполнение занимает 10 минут, решение придёт в кабинет.
            </p>
          </div>
          <Button href="/register" className="self-start" size="l">
            Оставить заявку
          </Button>
        </div>
      </Container>
    </section>
  );
}
