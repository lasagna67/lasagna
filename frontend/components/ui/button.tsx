import Link from "next/link";
import type { ComponentProps } from "react";

const variants = {
  primary: "bg-brand text-white hover:bg-brand-hover active:bg-brand-press",
  secondary: "bg-black text-white hover:bg-[#161616] active:bg-[#1b1b1b]",
  ghost: "bg-black/6 text-ink hover:bg-black/10",
} as const;

const sizes = {
  m: "h-10 px-5 text-[16px]",
  l: "h-14 px-8 text-[18px]",
} as const;

type Props = Omit<ComponentProps<typeof Link>, "className"> & {
  variant?: keyof typeof variants;
  size?: keyof typeof sizes;
  className?: string;
};

export function Button({ variant = "primary", size = "m", className = "", ...props }: Props) {
  return (
    <Link
      className={`inline-flex items-center justify-center rounded-full font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand ${variants[variant]} ${sizes[size]} ${className}`}
      {...props}
    />
  );
}
