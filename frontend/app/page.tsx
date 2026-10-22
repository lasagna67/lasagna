import { Calculator } from "@/components/landing/calculator";
import { Conditions } from "@/components/landing/conditions";
import { Cta } from "@/components/landing/cta";
import { Faq } from "@/components/landing/faq";
import { Footer } from "@/components/landing/footer";
import { Header } from "@/components/landing/header";
import { Hero } from "@/components/landing/hero";
import { Requirements } from "@/components/landing/requirements";
import { Steps } from "@/components/landing/steps";

export default function Home() {
  return (
    <>
      <Header />
      <main className="flex flex-col gap-16 pb-16 md:gap-24 md:pb-24">
        <Hero />
        <Conditions />
        <Steps />
        <Calculator />
        <Requirements />
        <Faq />
        <Cta />
      </main>
      <Footer />
    </>
  );
}
