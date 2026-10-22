import Link from "next/link";
import { formatDate, type BlogArticle } from "@/lib/blog/articles";

const plates = [
  "bg-plate-chardonnay hover:bg-[#f7c384]",
  "bg-plate-lagoon hover:bg-[#8ee8eb]",
  "bg-plate-menthol hover:bg-[#b2e699]",
] as const;

export function ArticleCard({ article, index }: { article: BlogArticle; index: number }) {
  return (
    <Link
      href={`/blog/${article.slug}`}
      className={`flex flex-col rounded-card-lg p-8 transition-colors ${plates[index % plates.length]}`}
    >
      <span className="text-[14px] text-ink-soft">
        {formatDate(article.date)} · {article.readingMinutes} мин чтения
      </span>
      <span className="mt-4 text-promo-s">{article.title}</span>
      <span className="mt-3 text-[16px] leading-6 text-ink-soft">{article.description}</span>
      <span className="mt-auto pt-6 font-medium text-brand">Читать →</span>
    </Link>
  );
}
