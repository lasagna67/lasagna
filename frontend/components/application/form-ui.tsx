import type { FieldError } from "@/lib/http";

export const input =
  "w-full rounded-2xl border border-black/10 px-4 py-3 text-[16px] outline-none focus:border-brand disabled:bg-black/4";
export const select = `${input} appearance-none`;
export const primary =
  "inline-flex h-12 items-center justify-center rounded-full bg-brand px-8 text-[17px] font-medium text-white transition-colors hover:bg-brand-hover disabled:opacity-60";
export const secondary =
  "inline-flex h-12 items-center justify-center rounded-full border border-black/15 px-7 text-[16px] transition-colors hover:border-brand hover:text-brand disabled:opacity-60";

export function label(text: string, control: React.ReactNode) {
  return (
    <label className="block">
      <span className="text-[14px] text-ink-soft">{text}</span>
      <span className="mt-1 block">{control}</span>
    </label>
  );
}

export function FieldErrors({ errors }: { errors?: FieldError[] }) {
  if (!errors?.length) return null;
  return (
    <ul className="rounded-2xl bg-plate-tomato/12 px-4 py-3 text-[14px] leading-5 text-ink">
      {errors.map((e, i) => (
        <li key={`${e.field}-${i}`}>
          {e.message}
          {e.field && <span className="font-mono text-[12px] text-ink-soft"> · {e.field}</span>}
        </li>
      ))}
    </ul>
  );
}

export function errorFields(e: unknown): FieldError[] {
  return e && typeof e === "object" && "fieldErrors" in e
    ? ((e as { fieldErrors: FieldError[] }).fieldErrors ?? [])
    : [];
}

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : "Неизвестная ошибка";
}
