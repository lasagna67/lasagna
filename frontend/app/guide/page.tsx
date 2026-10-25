import type { Metadata } from "next";
import { Footer } from "@/components/landing/footer";
import { Header } from "@/components/landing/header";
import { Guide } from "@/components/guide/guide";

export const metadata: Metadata = {
  title: "Инструкция",
  description:
    "Как войти в кабинет, типы кредитов, шаги заявки, статусы после отправки, подпись договора и формат CSV с оборотами.",
};

export default function GuidePage() {
  return (
    <>
      <Header />
      <Guide />
      <Footer />
    </>
  );
}
