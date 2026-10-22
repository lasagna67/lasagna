import { Container } from "@/components/ui/container";

const steps = [
  {
    title: "Заявка",
    text: "Заполните анкету: реквизиты подтянутся по ИНН, вопросы по финансам — за 10 минут.",
  },
  {
    title: "Проверка",
    text: "Запрашиваем выписку из ЕГРЮЛ или ЕГРИП и кредитную историю из БКИ онлайн.",
  },
  {
    title: "Решение",
    text: "Показываем оффер: сумма, срок, ставка и ежемесячный платёж. Примите его или запросите изменение.",
  },
  {
    title: "Подписание",
    text: "Подпишите договор электронно в кабинете — деньги придут на расчётный счёт.",
  },
];

export function Steps() {
  return (
    <section id="how" className="scroll-mt-24">
      <Container>
        <h2 className="text-promo-m">Как получить кредит</h2>
        <ol className="mt-8 grid gap-8 md:grid-cols-4 md:gap-6">
          {steps.map((step, i) => (
            <li key={step.title}>
              <span
                aria-hidden
                className="flex size-8 items-center justify-center rounded-full bg-brand text-[14px] font-semibold text-white"
              >
                {i + 1}
              </span>
              <h3 className="mt-4 text-[18px] font-semibold">{step.title}</h3>
              <p className="mt-2 text-[16px] leading-6 text-ink-soft">{step.text}</p>
            </li>
          ))}
        </ol>
      </Container>
    </section>
  );
}
