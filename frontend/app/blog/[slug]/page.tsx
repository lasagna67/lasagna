import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { ArticleBody } from "@/components/blog/article-body";
import { articles, getArticle } from "@/lib/blog/articles";
import { SITE_URL } from "@/lib/seo";

export function generateStaticParams() {
  return articles.map(({ slug }) => ({ slug }));
}

export async function generateMetadata({
  params,
}: PageProps<"/blog/[slug]">): Promise<Metadata> {
  const { slug } = await params;
  const article = getArticle(slug);
  if (!article) return {};

  return {
    title: article.title,
    description: article.description,
    alternates: { canonical: `/blog/${article.slug}` },
    openGraph: {
      type: "article",
      publishedTime: article.date,
      title: article.title,
      description: article.description,
      url: `/blog/${article.slug}`,
    },
  };
}

export default async function BlogArticlePage({ params }: PageProps<"/blog/[slug]">) {
  const { slug } = await params;
  const article = getArticle(slug);
  if (!article) notFound();

  const related = articles.filter(
    (item) => item.slug !== article.slug && item.tags.some((tag) => article.tags.includes(tag)),
  );
  const fallbackRelated = articles.filter((item) => item.slug !== article.slug);
  const relatedItems = [...related, ...fallbackRelated].slice(0, 2);

  const url = `${SITE_URL}/blog/${article.slug}`;
  const jsonLd = {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "Article",
        headline: article.title,
        description: article.description,
        datePublished: article.date,
        inLanguage: "ru-RU",
        url,
        mainEntityOfPage: url,
        author: { "@type": "Organization", name: "Lasagna Бизнес" },
        publisher: { "@type": "Organization", name: "Lasagna", url: SITE_URL },
      },
      {
        "@type": "BreadcrumbList",
        itemListElement: [
          { "@type": "ListItem", position: 1, name: "Главная", item: SITE_URL },
          { "@type": "ListItem", position: 2, name: "Блог", item: `${SITE_URL}/blog` },
          { "@type": "ListItem", position: 3, name: article.title, item: url },
        ],
      },
      ...(article.faq
        ? [
            {
              "@type": "FAQPage",
              mainEntity: article.faq.map(({ q, a }) => ({
                "@type": "Question",
                name: q,
                acceptedAnswer: { "@type": "Answer", text: a },
              })),
            },
          ]
        : []),
    ],
  };

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
      />
      <ArticleBody article={article} related={relatedItems} />
    </>
  );
}
