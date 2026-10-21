---
name: frontend-code-structure
description: Structure and compactness rules for React/Next.js + Tailwind code in this repo. Use when writing, reviewing, or refactoring frontend components, pages, and styles.
---

# Frontend Code Structure

## File layout

- Feature-oriented folders: `app/<route>/` for routes, `components/<feature>/` for shared UI, `lib/` for helpers and types.
- One exported component per file; filename matches the component name.
- Colocate `*.module.css`, tests, and helpers with the component they belong to.
- Route files (`page.tsx`, `layout.tsx`) stay thin: compose components, no business logic.

## Compactness

- Fewer lines win. 100 lines where 40 suffice is a defect. No boilerplate comments, no `// unused`, no commented-out code.
- No wrapper `<div>` without a reason (layout, semantics, styling). Prefer a single element with the right tag.
- Early return instead of nested conditionals: guard clauses first, happy path in the body.
- Repeated class strings go into a `const` at the top of the file (or a shared variant map). Conditional classes via `cn("...", cond && "...")`.
- No inline styles when a Tailwind utility exists; no one-off abstractions — extract only on the third repetition.
- Prefer plain functions and data (`const items = [...]` + `.map`) over components that only forward props.
- Client components only when they need state/effects/handlers; everything else stays a server component.
- No `useEffect` for derivable state — compute during render.

## Component size budget

- Target: a reader understands a component in one screen. Split above ~120 lines or when a block has its own visual/state identity.
- Props: primitives and small objects; more than ~5 props means the component is doing two jobs.
- Keep JSX depth ≤ 3 levels; deeper means extract the inner block.

## Consistency

- Match the surrounding code: same quote style, same import order (`next/*`, `react/*`, shared, local), no default exports except route files.
- Naming: components PascalCase, hooks `useX`, handlers `handleX`, booleans `isX`/`hasX`.
- Types live next to usage; no `any` — use `Props` interfaces or inferred types from helpers.

## Self-check before finishing

1. Is there dead or commented-out code?
2. Can any JSX block be extracted or flattened?
3. Are duplicated strings/classes deduplicated?
4. Is every import actually used?
