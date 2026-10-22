import type { ComponentProps } from "react";

export function Container({ className = "", ...props }: ComponentProps<"div">) {
  return <div className={`mx-auto w-full max-w-[1200px] px-4 md:px-8 ${className}`} {...props} />;
}
