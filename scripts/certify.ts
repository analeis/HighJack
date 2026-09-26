/**
 * HighJack certification pipeline.
 *
 * Runs the full validation gate list, then browser verification
 * (Playwright + axe), then records a report that identifies the exact
 * tree which was certified.
 *
 * Classification (see docs/development/CERTIFICATION.md):
 *   all validation gates pass          → VALIDATED
 *   + browser verification passes      → CERTIFIED
 *
 * Soundness rules this file enforces (audit OPS-1, OPS-9):
 *   - The report names the *certified tree*, not merely HEAD. A dirty
 *     working tree can never be classified CERTIFIED, because a report
 *     that names HEAD would attest to a commit that was never tested.
 *   - A skipped mandatory gate is a failure, not a softer pass. A release
 *     pipeline that gates on the exit code must not accept a build whose
 *     browser verification never ran.
 */
import { execSync } from 'node:child_process';
import { relative } from 'node:path';
import path from 'node:path';
import { runGates, printSummary, writeReport, ROOT, type GateResult } from './lib/gates.ts';
import { validationGates } from './lib/validation-gates.ts';
import { verificationGates } from './lib/verification-gates.ts';

function git(args: string): string {
  try {
    return execSync(`git ${args}`, { cwd: ROOT, encoding: 'utf8' }).trim();
  } catch {
    return 'unknown';
  }
}

interface TreeIdentity {
  /** The commit the working tree is based on, or 'unknown'. */
  commit: string;
  /** Hash of the staged/working tree content, or 'unknown'. */
  tree: string;
  /** Files differing from commit; empty means the tree is exactly commit. */
  dirty: string[];
  /** True when the working tree content equals the commit exactly. */
  clean: boolean;
}

/**
 * Identify the tree under certification.
 *
 * `git rev-parse HEAD` alone is insufficient: it names the last commit
 * even when the working tree has been modified, so a report could attest
 * to a commit whose content was never tested. The tree hash and the dirty
 * file list make that impossible to hide.
 */
function identifyTree(): TreeIdentity {
  const commit = git('rev-parse HEAD');
  const tree = git('write-tree');
  const status = git('status --porcelain');
  const dirty = status
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((line) => line.slice(3).trim())
    .filter((line) => line.length > 0);
  return { commit, tree, dirty, clean: dirty.length === 0 };
}

const identity = identifyTree();

console.log('HighJack certification');
console.log('======================');
console.log(`commit: ${identity.commit}`);
console.log(`tree:   ${identity.tree}`);
if (!identity.clean) {
  console.log(
    `\nWORKING TREE IS DIRTY — ${identity.dirty.length} path(s) differ from ${identity.commit.slice(0, 7)}:`,
  );
  for (const path of identity.dirty) console.log(`  ${path}`);
  console.log('\nA tree that differs from its commit cannot be certified: the report');
  console.log('would name a commit whose content was never tested. Commit or stash');
  console.log('your work, then re-run. To inspect without certifying, use `bun run validate`.');
  process.exit(1);
}
console.log(`started: ${new Date().toISOString()}`);

const validationResults = await runGates(validationGates, 'validation');
let verificationResults: GateResult[] = [];
if (validationResults.every((r) => r.status !== 'failed')) {
  verificationResults = await runGates(verificationGates, 'verification');
}

const all = [...validationResults, ...verificationResults];
const skipped = all.filter((r) => r.status === 'skipped');
// A skipped *mandatory* gate is already a failure (runGates records it as
// one). Only optional, environment-gated gates may be skipped.
const skippedMandatory = skipped.filter((r) => r.mandatory);
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
  commit: identity.commit,
  tree: identity.tree,
  clean: identity.clean,
  dirtyPaths: identity.dirty,
  finishedAt: new Date().toISOString(),
  skippedGates: skipped.map((r) => r.name),
  gates: all,
});

if (!ok) process.exit(1);

// A mandatory gate that did not run is a release blocker. `certify` exists to
// produce a CERTIFIED classification; if a mandatory gate was skipped the
// classification is at best VALIDATED, and the exit code must say so or a
// release pipeline gating on it will certify an unverified build.
if (skippedMandatory.length > 0) {
  console.error(
    `\n${skippedMandatory.length} mandatory gate(s) did not run: ${skippedMandatory.map((r) => r.name).join(', ')}`,
  );
  console.error(`classification is ${classification}, not CERTIFIED`);
  process.exit(1);
}

console.log(`\nclassification: ${classification}`);
console.log(`certified tree: ${relative('.', ROOT) || '.'} @ ${identity.tree.slice(0, 12)}`);
