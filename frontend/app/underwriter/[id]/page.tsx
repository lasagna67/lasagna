"use client";

import { Suspense, use } from "react";
import { Review } from "@/components/employee/review";

function ReviewRoute({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <Review id={id} />;
}

export default function UnderwriterReviewPage({ params }: { params: Promise<{ id: string }> }) {
  return (
    <Suspense
      fallback={<p className="mx-auto max-w-[1200px] px-4 py-10 text-ink-soft">Загружаем заявку…</p>}
    >
      <ReviewRoute params={params} />
    </Suspense>
  );
}
