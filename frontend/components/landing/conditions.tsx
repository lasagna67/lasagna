import { Container } from "@/components/ui/container";

const items = [
  {
    value: "до 30 млн ₽",
    title: "Сумма",
    text: "На оборотные средства, оборудование или расширение производства.",
    plate: "bg-plate-tomato",
  },
  {
    value: "до 5 лет",
    title: "Срок",
    text: "График под бизнес-цикл, досрочное погашение без комиссий.",
    plate: "bg-plate-menthol",
  },
  {
    value: "от 15,9%",
    title: "Ставка",
    text: "Индивидуально по результатам проверки данных и кредитной истории.",
    plate: "bg-plate-lagoon",
  },
];

export function Conditions() {
  return (
    <section id="conditions" className="scroll-mt-24">
      <Container>
        <h2 className="text-promo-m">Условия</h2>
        <div className="mt-8 grid gap-4 md:grid-cols-3">
          {items.map((item) => (
            <article key={item.title} className={`rounded-card p-8 ${item.plate}`}>
              <p className="text-[14px] text-ink-soft">{item.title}</p>
              <p className="mt-2 text-promo-m">{item.value}</p>
              <p className="mt-4 max-w-[32ch] text-[16px] leading-6">{item.text}</p>
            </article>
          ))}
        </div>
      </Container>
    </section>
  );
}
