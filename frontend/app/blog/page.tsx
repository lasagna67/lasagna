import type { Metadata } from "next";
import { ArticleCard } from "@/components/blog/article-card";
import { Container } from "@/components/ui/container";
import { articles } from "@/lib/blog/articles";

export const metadata: Metadata = {
  title: "Блог",
  description:
    "Статьи для предпринимателей: кредит для ИП, оборотные средства, кредитная история и БКИ. Практические гиды от Lasagna Бизнес.",
};

export default function BlogPage() {
  return (
    <>
      <Container className="py-12 md:py-16">
        <h1 className="text-promo-l">Блог</h1>
        <p className="mt-4 max-w-[56ch] text-[18px] leading-7 text-ink-soft">
          Разбираем, как устроен кредит для бизнеса: заявка, проверка данных, решение и деньги.
          Без банковского жаргона.
        </p>
        <div className="mt-10 grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {articles.map((article, index) => (
            <ArticleCard key={article.slug} article={article} index={index} />
          ))}
        </div>
      </Container>
    </>
  );
}
