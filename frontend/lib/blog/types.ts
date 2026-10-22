export type BlogFaq = {
  q: string;
  a: string;
};

export type BlogArticleMeta = {
  slug: string;
  title: string;
  description: string;
  date: string;
  tags: string[];
  readingMinutes: number;
  faq?: BlogFaq[];
};
