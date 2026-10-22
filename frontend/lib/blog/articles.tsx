import type { ComponentType } from "react";
import * as ipCredit from "@/content/blog/kredit-dlya-ip";
import * as workingCapital from "@/content/blog/oborotnyj-kredit";
import * as creditHistory from "@/content/blog/kreditnaya-istoriya-bki";
import type { BlogArticleMeta } from "./types";

type BlogModule = { meta: BlogArticleMeta; default: ComponentType };

const modules: BlogModule[] = [ipCredit, workingCapital, creditHistory];

export type BlogArticle = BlogArticleMeta & {
  Content: ComponentType;
};

export const articles: BlogArticle[] = modules
  .map(({ meta, default: Content }) => ({ ...meta, Content }))
  .sort((a, b) => b.date.localeCompare(a.date));

export function getArticle(slug: string): BlogArticle | undefined {
  return articles.find((a) => a.slug === slug);
}

const dateFormat = new Intl.DateTimeFormat("ru-RU", {
  day: "numeric",
  month: "long",
  year: "numeric",
});

export function formatDate(iso: string): string {
  return dateFormat.format(new Date(iso));
}
