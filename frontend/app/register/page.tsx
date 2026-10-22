import type { Metadata } from "next";
import { AuthFlow } from "@/components/auth/auth-flow";

export const metadata: Metadata = {
  title: "Регистрация",
  description: "Регистрация в кредитном кабинете и подключение компании по ИНН.",
};

export default function RegisterPage() {
  return <AuthFlow />;
}
