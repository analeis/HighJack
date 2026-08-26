/**
 * HighJack certification pipeline.
 *
 * Runs the full validation gate list, then browser verification
 * (Playwright + axe), then records a timestamped, commit-stamped report
 * with the resulting classification.
 *
 * Classification (see docs/development/CERTIFICATION.md):
 *   all validation gates pass          → VALIDATED
 *   + browser verification passes      → CERTIFIED
 */
import { execSync } from 'node:child_process';
import path from 'node:path';
import { runGates, printSummary, writeReport, type Gate, type GateResult } from './lib/gates.ts';

const ROOT = path.resolve(import.meta.dir, '..');

function gitCommit(): string {
  try {
    return execSync('git rev-parse HEAD', { cwd: ROOT, encoding: 'utf8' }).trim();
  } catch {
    return 'unknown';
  }
}

const validationGates: Gate[] = [
  { name: 'format', cmd: ['bunx', 'prettier', '--check', '.'] },
  { name: 'lint:eslint', cmd: ['bunx', 'eslint', 'packages', 'apps'] },
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

const verificationGates: Gate[] = [
  // Uses the production build produced by build:web above.
  {
    name: 'verify:browser',
    cmd: ['bunx', 'playwright', 'test'],
    cwd: 'apps/web',
    skip: () => process.env.HIGHJACK_SKIP_BROWSER === '1',
  },
];

console.log('HighJack certification');
console.log('======================');
console.log(`commit: ${gitCommit()}`);
console.log(`started: ${new Date().toISOString()}`);

const validationResults = await runGates(validationGates, 'validation');
let verificationResults: GateResult[] = [];
if (validationResults.every((r) => r.status !== 'failed')) {
  verificationResults = await runGates(verificationGates, 'verification');
}

const all = [...validationResults, ...verificationResults];
const ok = printSummary(all);

const validated =
  validationResults.every((r) => r.status === 'passed') && validationResults.length > 0;
const verified =
  validated &&
  verificationResults.length > 0 &&
  verificationResults.every((r) => r.status === 'passed');

const classification = !ok
  ? 'DEVELOPMENT'
  : verified
    ? 'CERTIFIED'
    : validated
      ? 'VALIDATED'
      : 'DEVELOPMENT';

writeReport({
  tool: 'highjack-certify',
  classification,
  commit: gitCommit(),
  finishedAt: new Date().toISOString(),
  skippedGates: all.filter((r) => r.status === 'skipped').map((r) => r.name),
  gates: all,
});

if (!ok) process.exit(1);
console.log(`\nclassification: ${classification}`);
