# Validation & Verification Pipeline

Validation and verification are deliberately separate concerns:

- **Validation** — is the repository structurally and technically valid?
  Fast, deterministic, runs on every commit.
- **Verification** — does the implementation actually satisfy the intended
  architecture and behavior? Slower, browser-level, runs before release.
- **Certification** — the recorded act of running both and classifying the
  build. See [CERTIFICATION.md](./CERTIFICATION.md).

## `bun run validate`

Single entry point; exits non-zero on the first failed gate.

```
validate
├── format        prettier --check (TS/TSX/CSS/MD/JSON + .astro via prettier-plugin-astro)
├── lint          eslint (packages+apps) · go vet ./...
├── gofmt         gofmt -l on server/ reports nothing
├── typecheck     tsc --noEmit (root project) · astro check
├── test:go       go test ./...
├── test:ts       vitest in packages/protocol, packages/ui, apps/game
├── build:web     astro build
├── build:game    vite build
├── build:server  go build ./...
├── assets        scripts/check-assets.ts
└── repo          scripts/repo-sanity.ts
```

### Asset gate (`scripts/check-assets.ts`)

- Allowed extensions only (`svg`, `md`, plus generated rasters when they
  exist), kebab-case naming, size ceilings per file.
- `assets/PROVENANCE.md` must exist and mention every asset directory.

### Repository sanity gate (`scripts/repo-sanity.ts`)

- Expected top-level structure present (apps, packages, server, docs, infra…).
- No committed secrets (pattern scan) and no `.env` files staged by accident
  (`.env.example` is the documented exception pattern).
- No files above hard size limits (catches accidental binaries/assets).
- Internal documentation links resolve (relative md links between docs).
- No empty directories tracked in git.

## `bun run certify`

Runs everything above **plus**:

1. Production website build served via `astro preview`.
2. Playwright suite: every route renders, navigation works desktop +
   mobile, no console errors, axe accessibility scans pass on key pages,
   responsive smoke checks.
3. Writes `verification/report.json`: timestamped gate results + final
   classification (`DEVELOPMENT` / `VALIDATED` / `VERIFIED` / `CERTIFIED`).

The report is reproducible for a given commit: same tree ⇒ same results.

## Go-only quick loop

```sh
gofmt -l ./server && go vet ./... && go test ./...
```

## Adding a new gate

Gates live in `scripts/lib/validation-gates.ts` as ordered entries with a
name and a shell command (or script). Both `scripts/validate.ts` and
`scripts/certify.ts` import that single list, so the two pipelines can
never drift apart. A gate that cannot run on CI must be marked
`optional` so it never silently fails a machine it cannot execute on —
but anything release-mandatory stays required.

## Toolchain pins & overrides

Decisions recorded here so they are not re-litigated by accident:

- **TypeScript stays on the 6.x line.** typescript-eslint does not support
  the TS 7 native port (it refuses to load); our lint config needs it, and
  nothing in the codebase needs TS 7 features. Revisit when
  typescript-eslint supports a stable TS ≥ 7.1.
- **`bun-types` (oven-sh), not `@types/bun`.** The `@types/bun` name on npm
  resolves to a DefinitelyTyped stub whose `/// <reference types="bun-types" />`
  does not resolve, silently dropping `import.meta.dir` and friends (masked
  further by `skipLibCheck`). The root tsconfig pins `"types": ["bun-types"]`.
- **`playwright-core` is overridden to a single version** in the root
  `package.json`. `@axe-core/playwright` and `@playwright/test` otherwise
  resolve two nominally incompatible `Page` types and root `tsc` rejects the
  e2e suite.
- **One major per tool across the workspace.** `vitest`, `jest-dom`, and
  `tailwindcss` ranges are kept identical in every workspace package; mixed
  majors previously produced duplicate `vite` (rollup vs rolldown plugin
  types) and duplicate `vitest` (`Assertion` augmentation hitting only one
  copy) failures in the root typecheck.
