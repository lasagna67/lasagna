import type { Metadata } from "next";
import { Onest } from "next/font/google";
import "./globals.css";

const onest = Onest({
  subsets: ["latin", "cyrillic"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-onest",
  display: "swap",
});

export const metadata: Metadata = {
  title: "Lasagna — кредит для бизнеса онлайн",
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
