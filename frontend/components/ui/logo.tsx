import type { ComponentProps } from "react";

type Props = Omit<ComponentProps<"span">, "children"> & { wordmark?: boolean };

export function Logo({ wordmark = true, className = "", ...props }: Props) {
  return (
    <span className={`flex items-center gap-2 ${className}`} {...props}>
      <svg viewBox="0 0 32 32" aria-hidden className="size-8 shrink-0">
        <rect width="32" height="32" rx="10" fill="#ef3124" />
        <rect x="7" y="9" width="18" height="3.5" rx="1.75" fill="#fff" />
        <rect x="7" y="14.25" width="18" height="3.5" rx="1.75" fill="#fff" />
        <rect x="7" y="19.5" width="11" height="3.5" rx="1.75" fill="#fff" />
      </svg>
      {wordmark && <span className="text-[20px] font-bold">Lasagna</span>}
    </span>
  );
}
