"use client";

import Link from "next/link";
import { Button } from "./button";
import { Logo } from "./logo";

type Props = {
  chip: string;
  chipClass?: string;
  email?: string;
  onLogout: () => void;
};

export function AppHeader({ chip, chipClass = "bg-plate-tomato", email, onLogout }: Props) {
  return (
    <header className="border-b border-black/4">
      <div className="mx-auto flex h-16 w-full max-w-[1200px] items-center gap-6 px-4 md:h-20 md:px-8">
        <Link href="/" className="flex items-center gap-2.5">
          <Logo />
          <span className={`rounded-full px-2 py-0.5 text-[12px] font-medium ${chipClass}`}>
            {chip}
          </span>
        </Link>
        <div className="ml-auto flex items-center gap-4">
          {email && <span className="hidden text-[14px] text-ink-soft sm:block">{email}</span>}
          <Button variant="ghost" onClick={onLogout}>
            Выйти
          </Button>
        </div>
      </div>
    </header>
  );
}
