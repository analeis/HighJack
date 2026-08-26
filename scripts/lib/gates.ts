/**
 * Shared gate runner for the validation / certification pipeline.
 *
 * A gate is a named shell command executed with inherited stdio so
 * failures are never hidden. Required gates abort the pipeline; optional
 * gates are recorded as skipped when their precondition is unmet.
 */
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';

export const ROOT = path.resolve(import.meta.dir, '..', '..');

export interface Gate {
  name: string;
  cmd: string[];
  cwd?: string;
  env?: Record<string, string>;
  /** Skipped (recorded, not failed) when this returns false. */
  skip?: () => boolean;
}

export interface GateResult {
  name: string;
  status: 'passed' | 'failed' | 'skipped';
  durationMs: number;
}

function runCommand(cmd: string[], cwd: string): Promise<number> {
  return new Promise((resolve) => {
    const child = spawn(cmd[0]!, cmd.slice(1), {
      cwd,
      stdio: 'inherit',
      env: { ...process.env },
    });
    child.on('exit', (code) => resolve(code ?? 1));
    child.on('error', () => resolve(1));
  });
}

const dim = (s: string) => `\x1b[2m${s}\x1b[0m`;
const green = (s: string) => `\x1b[32m${s}\x1b[0m`;
const red = (s: string) => `\x1b[31m${s}\x1b[0m`;
const yellow = (s: string) => `\x1b[33m${s}\x1b[0m`;
const bold = (s: string) => `\x1b[1m${s}\x1b[0m`;

export async function runGates(gates: Gate[], label: string): Promise<GateResult[]> {
  const results: GateResult[] = [];
  console.log(`\n${bold(`== ${label} ==`)}`);
  for (const gate of gates) {
    if (gate.skip?.()) {
      console.log(`${yellow('SKIP')} ${gate.name}`);
      results.push({ name: gate.name, status: 'skipped', durationMs: 0 });
      continue;
    }
    const start = performance.now();
    process.stdout.write(`${dim('RUN ')} ${gate.name} ${dim(`· ${gate.cmd.join(' ')}`)}\n`);
    const code = await runCommand(gate.cmd, gate.cwd ? path.join(ROOT, gate.cwd) : ROOT);
    const durationMs = Math.round(performance.now() - start);
    if (code === 0) {
      console.log(`${green('OK  ')} ${gate.name} ${dim(`(${durationMs}ms)`)}\n`);
      results.push({ name: gate.name, status: 'passed', durationMs });
    } else {
      console.error(`${red('FAIL')} ${gate.name} ${dim(`(${durationMs}ms, exit ${code})`)}\n`);
      results.push({ name: gate.name, status: 'failed', durationMs });
      // Fail fast: later gates depend on earlier ones being sound.
      for (const remaining of gates.slice(gates.indexOf(gate) + 1)) {
        results.push({ name: remaining.name, status: 'skipped', durationMs: 0 });
      }
      break;
    }
  }
  return results;
}

export function printSummary(results: GateResult[]): boolean {
  const failed = results.filter((r) => r.status === 'failed');
  const skipped = results.filter((r) => r.status === 'skipped');
  console.log(bold('\n== summary =='));
  for (const r of results) {
    const mark =
      r.status === 'passed' ? green('PASS') : r.status === 'failed' ? red('FAIL') : yellow('SKIP');
    console.log(`${mark} ${r.name}`);
  }
  if (failed.length > 0) {
    console.error(red(`\n${failed.length} gate(s) failed`));
    return false;
  }
  if (skipped.length > 0) {
    console.log(yellow(`\n${skipped.length} gate(s) skipped (see above)`));
  }
  console.log(green('\nall required gates passed'));
  return true;
}

export function writeReport(payload: Record<string, unknown>): void {
  const dir = path.join(ROOT, 'verification');
  mkdirSync(dir, { recursive: true });
  const file = path.join(dir, 'report.json');
  writeFileSync(file, JSON.stringify(payload, null, 2) + '\n');
  console.log(dim(`\nreport written to ${path.relative(ROOT, file)}`));
}

/** True when a playwright browser is already available for this machine. */
export function hasPlaywrightBrowser(): boolean {
  return (
    existsSync(path.join(process.env.HOME ?? '', '.cache', 'ms-playwright')) ||
    process.env.PLAYWRIGHT_BROWSERS_PATH !== undefined
  );
}
