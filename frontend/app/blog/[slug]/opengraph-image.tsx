import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { ImageResponse } from "next/og";
import { getArticle } from "@/lib/blog/articles";

export const alt = "Статья блога Lasagna Бизнес";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

const onest = await readFile(join(process.cwd(), "app/blog/onest-400.ttf"));

export default async function Image({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  const article = getArticle(slug);

  return new ImageResponse(
    (
      <div
        style={{
          display: "flex",
          width: "100%",
          height: "100%",
          flexDirection: "column",
          padding: 64,
          backgroundColor: "#ffcf99",
          color: "#2a2c2f",
          fontFamily: "Onest",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <div
            style={{
              display: "flex",
              width: 56,
              height: 56,
              borderRadius: 16,
              backgroundColor: "#ef3124",
              flexDirection: "column",
              justifyContent: "center",
              padding: "0 12px",
              gap: 6,
            }}
          >
            <div style={{ display: "flex", height: 8, borderRadius: 4, backgroundColor: "#fff" }} />
            <div style={{ display: "flex", height: 8, borderRadius: 4, backgroundColor: "#fff" }} />
            <div style={{ display: "flex", height: 8, borderRadius: 4, backgroundColor: "#fff" }} />
          </div>
          <div style={{ display: "flex", fontSize: 32 }}>Lasagna Бизнес</div>
        </div>
        <div
          style={{
            display: "flex",
            marginTop: 56,
            fontSize: 64,
            lineHeight: 1.15,
            maxWidth: 1000,
          }}
        >
          {article?.title ?? "Блог Lasagna"}
        </div>
        <div
          style={{
            display: "flex",
            marginTop: "auto",
            fontSize: 26,
            color: "#737373",
          }}
        >
          Блог · кредит для бизнеса
        </div>
      </div>
    ),
    {
      ...size,
      fonts: [
        { name: "Onest", data: onest, weight: 400 as const, style: "normal" as const },
      ],
    },
  );
}
