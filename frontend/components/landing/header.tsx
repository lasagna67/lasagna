import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { Logo } from "@/components/ui/logo";

const nav = [
  { href: "/#conditions", label: "Условия" },
  { href: "/#how", label: "Как получить" },
  { href: "/guide", label: "Инструкция" },
  { href: "/#calculator", label: "Калькулятор" },
  { href: "/#faq", label: "Вопросы" },
  { href: "/blog", label: "Блог" },
];

export function Header() {
  return (
    <header className="sticky top-0 z-50 border-b border-black/4 bg-white/90 backdrop-blur-md">
      <Container className="flex h-16 items-center gap-8 md:h-20">
        <Link href="/" className="flex items-center gap-2.5">
          <Logo />
          <span className="rounded-full bg-plate-tomato px-2 py-0.5 text-[12px] font-medium">
            Бизнес
          </span>
        </Link>
        <nav aria-label="Основная навигация" className="hidden items-center gap-6 md:flex">
          {nav.map((item) => (
            <Link key={item.href} href={item.href} className="text-[16px] hover:text-brand">
              {item.label}
            </Link>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-3">
          <Link href="/login" className="hidden text-[16px] font-medium hover:text-brand sm:block">
            Войти
          </Link>
          <Button href="#apply">Оставить заявку</Button>
        </div>
      </Container>
    </header>
  );
}
