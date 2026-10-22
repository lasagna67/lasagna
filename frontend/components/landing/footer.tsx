import Link from "next/link";
import { Container } from "@/components/ui/container";

const links = [
  { href: "#conditions", label: "Условия" },
  { href: "#how", label: "Как получить" },
  { href: "#calculator", label: "Калькулятор" },
  { href: "#faq", label: "Вопросы" },
];

export function Footer() {
  return (
    <footer className="border-t border-black/8">
      <Container className="flex flex-col gap-8 py-12 md:flex-row md:justify-between">
        <div>
          <p className="text-[20px] font-bold">Lasagna Бизнес</p>
          <p className="mt-2 max-w-[36ch] text-[14px] text-ink-soft">
            Кредитный кабинет для предпринимателей: заявка, решение и подписание онлайн.
          </p>
        </div>
        <nav aria-label="Навигация в подвале" className="flex flex-col gap-3">
          {links.map((item) => (
            <Link key={item.href} href={item.href} className="text-[16px] hover:text-brand">
              {item.label}
            </Link>
          ))}
        </nav>
        <div className="max-w-[420px] text-[14px] leading-5 text-ink-soft">
          <p>
            Демонстрационный проект. Условия на странице приведены как пример и не являются
            публичной офертой и предложением банковских услуг.
          </p>
          <p className="mt-4">© 2026 Lasagna</p>
        </div>
      </Container>
    </footer>
  );
}
