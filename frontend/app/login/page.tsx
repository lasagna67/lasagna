import type { Metadata } from "next";
import { AuthFlow } from "@/components/auth/auth-flow";

export const metadata: Metadata = {
  title: "Вход",
  description: "Вход в кредитный кабинет: выбор демо-сценария или регистрация новой компании.",
};

export default function LoginPage() {
  return <AuthFlow />;
}
