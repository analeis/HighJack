/**
 * Shared gate runner for the validation / certification pipeline.
 *
 * A gate is a named shell command executed with inherited stdio so
 * failures are never hidden. Required gates abort the pipeline; optional
 * gates are recorded as skipped when their precondition is unmet.
 */
import { spawn } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';

export const ROOT = path.resolve(import.meta.dir, '..', '..');

export interface Gate {
  name: string;
  cmd: string[];
  cwd?: string;
  env?: Record<string, string>;
  /**
   * Skipped (recorded, not failed) when this returns true.
   *
   * A gate that is skipped while `mandatory` is true (the default) fails the
   * pipeline, so a mandatory check can never be switched off without both
   * the report and the exit code saying so. Optional gates exist for
   * environment-gated coverage (the PostgreSQL suite when no test database is
   * configured); they are recorded as skipped, never as passed.
   */
  skip?: () => boolean;
  /** Defaults to true. Set false only for genuinely environment-gated coverage. */
  mandatory?: boolean;
  /**
   * How long this gate may run before it is killed, in milliseconds.
   *
   * Defaults to fifteen minutes, which every gate in this repository clears in
   * well under a minute. A gate that needs longer should say so here rather
   * than by raising the default for every other gate.
   */
  timeoutMs?: number;
}

export interface GateResult {
  name: string;
  status: 'passed' | 'failed' | 'skipped';
  durationMs: number;
  /** Whether skipping this gate is tolerated. Mirrors Gate.mandatory. */
  mandatory: boolean;
  /**
   * Why a gate did not run, when it did not.
   *
   * The two are different facts and the report used to blur them: a gate whose
   * precondition was unmet, and a gate that never ran because an earlier one
   * failed. Only the first is about the environment.
   */
  skipReason?: 'precondition' | 'after-failure';
}

/**
 * How long a gate may run before it is killed.
 *
 * A gate that hangs used to hang the whole certification, with no diagnosis and
 * no way out except killing the terminal. That is the worst failure mode a
 * pipeline can have: it does not say what went wrong, and it does not stop.
 *
 * The budget is generous — every gate in this repository finishes in well under
 * a minute — so the timeout is a backstop against a hang, not a limit on slow
 * work.
 */
const DEFAULT_GATE_TIMEOUT_MS = 15 * 60 * 1000;

interface CommandOutcome {
  code: number;
  timedOut: boolean;
}

function runCommand(cmd: string[], cwd: string, timeoutMs: number): Promise<CommandOutcome> {
  return new Promise((resolve) => {
    const child = spawn(cmd[0]!, cmd.slice(1), {
      cwd,
      stdio: 'inherit',
      env: { ...process.env },
    });
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      // SIGKILL rather than SIGTERM: a gate that ignores a polite request to
      // stop is exactly the case this exists for.
      child.kill('SIGKILL');
    }, timeoutMs);
    const finish = (code: number): void => {
      clearTimeout(timer);
      resolve({ code, timedOut });
    };
    child.on('exit', (code) => finish(code ?? 1));
    child.on('error', () => finish(1));
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
    const mandatory = gate.mandatory ?? true;
    if (gate.skip?.()) {
      if (mandatory) {
        console.error(`${red('SKIP')} ${gate.name} ${dim('(mandatory gate cannot be skipped)')}\n`);
        results.push({
          name: gate.name,
          status: 'failed',
          durationMs: 0,
          mandatory,
          skipReason: 'precondition',
        });
        for (const remaining of gates.slice(gates.indexOf(gate) + 1)) {
          results.push({
            name: remaining.name,
            status: 'skipped',
            durationMs: 0,
            mandatory: remaining.mandatory ?? true,
            skipReason: 'after-failure',
          });
        }
        break;
      }
      console.log(`${yellow('SKIP')} ${gate.name} ${dim('(optional, precondition unmet)')}`);
      results.push({
        name: gate.name,
        status: 'skipped',
        durationMs: 0,
        mandatory,
        skipReason: 'precondition',
      });
      continue;
    }
    const start = performance.now();
    process.stdout.write(`${dim('RUN ')} ${gate.name} ${dim(`· ${gate.cmd.join(' ')}`)}\n`);
    const outcome = await runCommand(
      gate.cmd,
      gate.cwd ? path.join(ROOT, gate.cwd) : ROOT,
      gate.timeoutMs ?? DEFAULT_GATE_TIMEOUT_MS,
    );
    const durationMs = Math.round(performance.now() - start);
    if (outcome.timedOut) {
      console.error(`${red('TIMEOUT')} ${gate.name} ${dim(`(killed after ${durationMs}ms)`)}\n`);
      console.error(
        `  the gate "${gate.name}" did not finish and was killed. If it is genuinely slow, ` +
          'give it a longer budget in scripts/lib/validation-gates.ts.\n',
      );
    }
    if (outcome.code === 0 && !outcome.timedOut) {
      console.log(`${green('OK  ')} ${gate.name} ${dim(`(${durationMs}ms)`)}\n`);
      results.push({ name: gate.name, status: 'passed', durationMs, mandatory });
    } else {
      if (!outcome.timedOut) {
        console.error(
          `${red('FAIL')} ${gate.name} ${dim(`(${durationMs}ms, exit ${outcome.code})`)}\n`,
        );
      }
      results.push({ name: gate.name, status: 'failed', durationMs, mandatory });
      // Fail fast: later gates depend on earlier ones being sound.
      for (const remaining of gates.slice(gates.indexOf(gate) + 1)) {
        results.push({
          name: remaining.name,
          status: 'skipped',
          durationMs: 0,
          mandatory: remaining.mandatory ?? true,
          skipReason: 'after-failure',
        });
      }
      break;
    }
  }
  return results;
}

export function printSummary(results: GateResult[]): boolean {
  const failed = results.filter((r) => r.status === 'failed');
  // Only optional gates may be skipped; a mandatory skip is already recorded
  // as failed by runGates, and is surfaced here for a complete report.
  const skipped = results.filter((r) => r.status === 'skipped');
  const skippedOptional = skipped.filter((r) => !r.mandatory);
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
  if (skippedOptional.length > 0) {
    console.log(
      yellow(
        `\n${skippedOptional.length} optional gate(s) skipped: ${skippedOptional.map((r) => r.name).join(', ')}`,
      ),
    );
    console.log(yellow('these are recorded as skipped, not passed'));
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
