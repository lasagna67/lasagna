"use client";

import { api } from "@/lib/api";

function openUrl(url: string) {
  window.open(url, "_blank");
  setTimeout(() => URL.revokeObjectURL(url), 120_000);
}

export async function openDocument(docId: string, token: string) {
  const blob = await api.document(docId, token);
  openUrl(URL.createObjectURL(blob));
}

export async function downloadSignature(id: string, token: string) {
  const signature = await api.signature(id, token);
  const blob = new Blob([JSON.stringify(signature, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "contract-signature.json";
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

export async function documentUrl(docId: string, token: string): Promise<string> {
  const blob = await api.document(docId, token);
  return URL.createObjectURL(blob);
}
