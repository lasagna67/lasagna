import type { Metadata } from "next";
import { Onest } from "next/font/google";
import { SITE_URL } from "@/lib/seo";
import "./globals.css";

const onest = Onest({
  subsets: ["latin", "cyrillic"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-onest",
  display: "swap",
});

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: {
    default: "Lasagna — кредит для бизнеса онлайн",
    template: "%s — Lasagna Бизнес",
  },
  description:
    "Заявка за 10 минут, решение по данным из ЕГРЮЛ и кредитной истории, подписание документов в кабинете. Для ИП и ООО.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="ru" className={`${onest.variable} h-full antialiased`}>
      <body className="min-h-full bg-white font-sans text-ink">{children}</body>
    </html>
  );
}
