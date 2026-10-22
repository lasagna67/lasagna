import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { Logo } from "@/components/ui/logo";

export default function BlogLayout({ children }: LayoutProps<"/blog">) {
  return (
    <>
      <header className="border-b border-black/4">
        <Container className="flex h-16 items-center gap-6 md:h-20">
          <Link href="/" className="flex items-center gap-2.5">
            <Logo />
            <span className="rounded-full bg-plate-tomato px-2 py-0.5 text-[12px] font-medium">
              Бизнес
            </span>
          </Link>
          <Link href="/blog" className="text-[16px] hover:text-brand">
            Блог
          </Link>
          <Button href="/#apply" className="ml-auto">
            Оставить заявку
          </Button>
        </Container>
      </header>
      {children}
      <footer className="border-t border-black/8">
        <Container className="py-8 text-[14px] text-ink-soft">
          © 2026 Lasagna · Демонстрационный проект
        </Container>
      </footer>
    </>
  );
}
