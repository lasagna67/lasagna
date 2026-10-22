import Link from "next/link";
import { Button } from "@/components/ui/button";
import { formatDate, type BlogArticle } from "@/lib/blog/articles";
import { Breadcrumbs } from "./breadcrumbs";

export function ArticleBody({ article, related }: { article: BlogArticle; related: BlogArticle[] }) {
  return (
    <div className="mx-auto w-full max-w-[720px] px-4 pb-20 md:px-6">
      <Breadcrumbs
        items={[
          { label: "Главная", href: "/" },
          { label: "Блог", href: "/blog" },
          { label: article.title },
        ]}
      />
      <article>
        <h1 className="mt-6 text-promo-l">{article.title}</h1>
        <p className="mt-4 text-[14px] text-ink-soft">
          <time dateTime={article.date}>{formatDate(article.date)}</time> ·{" "}
          {article.readingMinutes} мин чтения
        </p>
        <p className="mt-6 text-[18px] leading-7">{article.description}</p>
        <div className="mt-8 space-y-5 text-[16px] leading-7 [&_a]:font-medium [&_a]:text-brand [&_a:hover]:underline [&_h2]:mt-10 [&_h2]:text-promo-s [&_li]:pl-1 [&_ol]:list-decimal [&_ol]:pl-5 [&_ul]:list-disc [&_ul]:pl-5">
          <article.Content />
        </div>
      </article>

      {article.faq && (
        <section aria-label="Частые вопросы" className="mt-14">
          <h2 className="text-promo-s">Частые вопросы</h2>
          <div className="mt-6 border-t border-black/8">
            {article.faq.map(({ q, a }) => (
              <details key={q} className="group border-b border-black/8">
                <summary className="flex cursor-pointer list-none items-center justify-between gap-6 py-5 text-[16px] font-medium [&::-webkit-details-marker]:hidden">
                  {q}
                  <span
                    aria-hidden
                    className="text-[24px] leading-none text-brand transition-transform group-open:rotate-45"
                  >
                    +
                  </span>
                </summary>
                <p className="max-w-[70ch] pb-5 text-[16px] leading-6 text-ink-soft">{a}</p>
              </details>
            ))}
          </div>
        </section>
      )}

      <div className="mt-14 flex flex-col gap-6 rounded-card-lg bg-plate-tomato p-8 sm:flex-row sm:items-center sm:justify-between md:p-10">
        <div>
          <p className="text-promo-s">Готовы подать заявку?</p>
          <p className="mt-2 max-w-[40ch] text-[16px] leading-6">
            Заполните анкету за 10 минут — решение придёт в кабинет.
          </p>
        </div>
        <Button href="/#apply" className="self-start" size="l">
          Оставить заявку
        </Button>
      </div>

      {related.length > 0 && (
        <nav aria-label="Читайте также" className="mt-14">
          <h2 className="text-promo-s">Читайте также</h2>
          <ul className="mt-6 space-y-3">
            {related.map((item) => (
              <li key={item.slug}>
                <Link href={`/blog/${item.slug}`} className="text-[16px] hover:text-brand">
                  {item.title}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
      )}
    </div>
  );
}
