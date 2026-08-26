/**
 * HighJack unified validation pipeline.
 *
 * Runs every mandatory gate in dependency order and fails fast.
 * See docs/development/VALIDATION.md for the gate reference.
 */
import { runGates, printSummary, type Gate } from './lib/gates.ts';

const gates: Gate[] = [
  { name: 'format', cmd: ['bunx', 'prettier', '--check', '.'] },
  {
    name: 'lint:eslint',
    cmd: ['bunx', 'eslint', 'packages', 'apps'],
  },
  { name: 'lint:gofmt', cmd: ['bash', '-c', 'test -z "$(gofmt -l ./server)" && echo gofmt clean'] },
  { name: 'lint:govet', cmd: ['go', 'vet', './...'] },
  { name: 'typecheck:tsc', cmd: ['bunx', 'tsc', '--noEmit', '-p', 'tsconfig.json'] },
  { name: 'typecheck:astro', cmd: ['bun', 'run', 'check'], cwd: 'apps/web' },
  {
    name: 'test:ts',
    cmd: [
      'bash',
      '-c',
      'for d in packages/protocol packages/ui apps/game; do (cd $d && bunx vitest run) || exit 1; done',
    ],
  },
  { name: 'test:go', cmd: ['go', 'test', './...'] },
  { name: 'build:web', cmd: ['bun', 'run', 'build'], cwd: 'apps/web' },
  { name: 'build:game', cmd: ['bun', 'run', 'build'], cwd: 'apps/game' },
  {
    name: 'build:server',
    cmd: ['go', 'build', '-o', '/tmp/opencode/highjack-server-cert', './server/cmd/highjack'],
  },
  { name: 'assets', cmd: ['bun', 'scripts/check-assets.ts'] },
  { name: 'repo', cmd: ['bun', 'scripts/repo-sanity.ts'] },
];

const results = await runGates(gates, 'validation');
process.exit(printSummary(results) ? 0 : 1);
