/**
 * The single definition of the HighJack validation gate list.
 *
 * Shared by `bun run validate` (scripts/validate.ts) and `bun run certify`
 * (scripts/certify.ts) so the two pipelines can never drift apart.
 * Gate order is dependency order; the runner fails fast.
 *
 * CI (.github/workflows/ci.yml) invokes the same gates by name via
 * `bun run gate <name>`, so the checked-in pipeline and the local pipeline
 * cannot diverge either.
 */
import { tmpdir } from 'node:os';
import path from 'node:path';

import type { Gate } from './gates.ts';

/**
 * The PostgreSQL suite. It is genuinely optional when no test database is
 * configured, but once `HIGHJACK_TEST_DATABASE_URL` is set it becomes
 * mandatory: offering a database and then silently not testing against it is
 * worse than not offering one.
 *
 * The whole package is run, with no `-run` filter. A name filter can match
 * zero tests and still exit 0, which previously caused CI to skip the JSONB
 * round-trip and the interrupted-match policy tests while the step reported
 * success (audit OPS-2).
 */
const databaseGate: Gate = {
  name: 'test:go:db',
  cmd: ['go', 'test', '-count=1', './server/internal/persistence/...'],
  mandatory: process.env.HIGHJACK_TEST_DATABASE_URL !== undefined,
  skip: () => process.env.HIGHJACK_TEST_DATABASE_URL === undefined,
};

export const validationGates: Gate[] = [
  {
    name: 'format',
    // The root dependencies have to be installed. `bunx` falls back to fetching
    // the package from the registry when it is not present locally, so a missing
    // install would silently format with whatever version the registry served
    // that day. The check names the cause instead.
    cmd: [
      'bun',
      'format',
    ],
  },
  {
    name: 'lint:oxlint',
    // `scripts` is linted too. It holds the certification pipeline — the thing
    // that produces the evidence for every other claim — and it was
    // type-checked but never linted, so a defect in it would have been found by
    // no gate at all.
    //
    // Unlike ESLint, oxlint resolves `.astro` frontmatter, so the website is
    // linted for the first time. It used to be skipped entirely: the ESLint
    // configuration matched only `**/*.{ts,tsx,mts}`, and ESLint reported every
    // `.astro` file as "ignored because no matching configuration was supplied".
    // In CI the output is formatted for GitHub, which turns a lint failure
    // into an annotation on the offending line of the pull request rather than a
    // wall of text in the job log. Everywhere else the readable default is
    // better, so the format follows the environment.
    cmd: [
      'bash',
      '-c',
      'test -d node_modules/oxlint || { echo "oxlint: the root dependencies are not installed."; echo "Run \\`bun install\\` at the repository root."; exit 1; }; bunx oxlint packages apps scripts ${GITHUB_ACTIONS:+--format=github}',
    ],
  },
  { name: 'lint:gofmt', cmd: ['bash', '-c', 'test -z "$(gofmt -l ./server)" && echo gofmt clean'] },
  { name: 'lint:govet', cmd: ['go', 'vet', './...'] },
  {
    name: 'typecheck:ts',
    // `bun check` is Bun's own typechecker: it reads the same tsconfig, reports
    // the same errors, and needs no TypeScript package at all. It is about
    // eighteen times faster than the reference compiler on this repository.
    cmd: ['bun', 'check', '-p', 'tsconfig.json'],
  },
  {
    name: 'typecheck:reference',
    // The reference typechecker, alongside the fast one.
    //
    // Requiring *both* to be clean is strictly safer than either alone: a false
    // negative in Bun's checker is caught by the reference, and a false positive
    // in one is caught by the other. The pair is the evidence that the fast gate
    // is equivalent to the reference on this codebase.
    //
    // `tsc6` comes from the `@typescript/typescript6` compatibility package, the
    // same one `astro check` uses — it is the only TypeScript with a
    // programmatic API until TypeScript 7.1, and Astro's language server needs
    // one. So the package earns its place twice.
    cmd: ['bunx', 'tsc6', '--noEmit', '-p', 'tsconfig.json'],
  },
  { name: 'typecheck:astro', cmd: ['bun', 'run', 'check'], cwd: 'apps/web' },
  {
    name: 'test:ts',
    cmd: [
      'bash',
      '-c',
      'for d in packages/protocol packages/ui apps/game; do (cd $d && bunx vitest run) || exit 1; done',
    ],
  },
  // -count=1 defeats the test cache, so a green gate always means the suite
  // actually ran in this certification. -race because the match runtime and
  // the realtime handler are concurrent by design; CI already required it and
  // certification previously did not.
  { name: 'test:go', cmd: ['go', 'test', '-count=1', '-race', './server/...'] },
  databaseGate,
  { name: 'audit:deps', cmd: ['bun', 'audit'] },
  {
    name: 'audit:govuln',
    cmd: ['go', 'run', 'golang.org/x/vuln/cmd/govulncheck@v1.9.0', './server/...'],
  },
  { name: 'build:web', cmd: ['bun', 'run', 'build'], cwd: 'apps/web' },
  { name: 'build:game', cmd: ['bun', 'run', 'build'], cwd: 'apps/game' },
  {
    name: 'build:server',
    // The binary is a build artifact, so it goes to the OS temp directory rather
    // than a path baked into the repository. The gate only proves the server
    // compiles; nothing consumes the file afterwards.
    cmd: [
      'go',
      'build',
      '-o',
      path.join(tmpdir(), 'highjack-server-cert'),
      './server/cmd/highjack',
    ],
  },
  { name: 'assets', cmd: ['bun', 'scripts/check-assets.ts'] },
  { name: 'repo', cmd: ['bun', 'scripts/repo-sanity.ts'] },
];
