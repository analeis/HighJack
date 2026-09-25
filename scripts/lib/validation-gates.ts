/**
 * The single definition of the HighJack validation gate list.
 *
 * Shared by `bun run validate` (scripts/validate.ts) and `bun run certify`
 * (scripts/certify.ts) so the two pipelines can never drift apart.
 * Gate order is dependency order; the runner fails fast.
 */
import type { Gate } from './gates.ts';

export const validationGates: Gate[] = [
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
