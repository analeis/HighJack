/**
 * Repository sanity gate.
 *
 * Structural checks that keep the repository honest:
 * - required structure exists
 * - no committed secrets / env files
 * - file size ceilings (catches stray binaries)
 * - documentation links resolve
 * - no build outputs or junk tracked in git
 */
import { execSync } from 'node:child_process';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir, '..');
const problems: string[] = [];

// ---- expected structure ------------------------------------------------------

const REQUIRED_PATHS = [
  'package.json',
  'bun.lock',
  'go.mod',
  'LICENSE',
  'README.md',
  '.gitignore',
  'apps/web/package.json',
  'apps/game/package.json',
  'packages/protocol/package.json',
  'packages/ui/package.json',
  'server/cmd/highjack/main.go',
  'server/migrations/0001_init.sql',
  'docs/architecture/OVERVIEW.md',
  'docs/architecture/FRONTENDS.md',
  'docs/architecture/BACKEND.md',
  'docs/architecture/GAME_ENGINE.md',
  'docs/design/ART_DIRECTION.md',
  'docs/design/DESIGN_SYSTEM.md',
  'docs/design/UI_PRINCIPLES.md',
  'docs/protocol/OVERVIEW.md',
  'docs/game/GAME_DESIGN.md',
  'docs/development/SETUP.md',
  'docs/development/TESTING.md',
  'docs/development/VALIDATION.md',
  'docs/development/CERTIFICATION.md',
  'infra/docker/Dockerfile.server',
  'infra/cloudflare/wrangler.toml',
  '.github/workflows/ci.yml',
  'assets/PROVENANCE.md',
];
for (const p of REQUIRED_PATHS) {
  if (!existsSync(path.join(ROOT, p))) {
    problems.push(`required path missing: ${p}`);
  }
}

// ---- secrets scan ------------------------------------------------------------

const SECRET_PATTERNS: [string, RegExp][] = [
  ['private key block', /-----BEGIN [A-Z ]*PRIVATE KEY-----/],
  ['AWS access key', /AKIA[0-9A-Z]{16}/],
  ['GitHub token', /gh[pousr]_[A-Za-z0-9]{30,}/],
  ['OpenAI-style key', /sk-[A-Za-z0-9]{20,}/],
  ['slack token', /xox[baprs]-[A-Za-z0-9-]{10,}/],
];

const FORBIDDEN_FILES = [/\.env$/, /id_rsa/, /\.pem$/];

function gitFiles(extraArgs: string): string[] {
  try {
    return execSync(`git ls-files ${extraArgs}`, { cwd: ROOT, encoding: 'utf8' })
      .split('\n')
      .filter(Boolean);
  } catch {
    return [];
  }
}

// Scan everything git would carry: tracked files plus untracked-but-not-
// ignored files, so the gate is effective before an initial commit too.
const candidates = new Set([...gitFiles('--cached'), ...gitFiles('--others --exclude-standard')]);
const tracked = [...candidates].sort();
if (tracked.length === 0) {
  problems.push('could not enumerate repository files for the sanity scan');
}
for (const rel of tracked) {
  if (FORBIDDEN_FILES.some((re) => re.test(rel)) && !rel.endsWith('.env.example')) {
    problems.push(`forbidden file tracked in git: ${rel}`);
  }
  const full = path.join(ROOT, rel);
  let stat;
  try {
    stat = statSync(full);
  } catch {
    continue;
  }
  // Size ceiling: lockfiles exempt (they are generated but intentionally committed).
  const isLockfile = /(^|\/)(bun\.lock|go\.sum)$/.test(rel);
  if (!isLockfile && stat.size > 1024 * 1024) {
    problems.push(
      `${rel}: ${(stat.size / 1024 / 1024).toFixed(1)} MB exceeds the 1 MB sanity ceiling`,
    );
  }
  // Content scan for small text files only.
  if (
    stat.isFile() &&
    stat.size < 512 * 1024 &&
    /\.(ts|tsx|js|mjs|json|md|toml|yml|yaml|sql|go|mod|css|html|sh)$|Dockerfile/.test(rel)
  ) {
    let text: string;
    try {
      text = readFileSync(full, 'utf8');
    } catch {
      continue;
    }
    for (const [label, re] of SECRET_PATTERNS) {
      if (re.test(text)) {
        problems.push(`${rel}: possible ${label} detected`);
      }
    }
  }
}

// ---- documentation link integrity ---------------------------------------------

function walkMarkdown(dir: string, files: string[] = []): string[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return files;
  }
  for (const entry of entries) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) walkMarkdown(full, files);
    else if (entry.endsWith('.md')) files.push(full);
  }
  return files;
}

for (const doc of walkMarkdown(path.join(ROOT, 'docs'))) {
  const content = readFileSync(doc, 'utf8');
  const linkRe = /\[[^\]]*\]\(([^)#\s]+)(?:#[^)]*)?\)/g;
  let match: RegExpExecArray | null;
  while ((match = linkRe.exec(content)) !== null) {
    const target = match[1]!;
    if (/^[a-z]+:\/\//.test(target) || !target.includes('.')) continue; // external or anchor-only
    const resolved = path.resolve(path.dirname(doc), decodeURIComponent(target));
    if (!existsSync(resolved)) {
      problems.push(`${path.relative(ROOT, doc)}: broken relative link → ${target}`);
    }
  }
}

// ---- report -------------------------------------------------------------------

if (problems.length > 0) {
  console.error('repository sanity check failed:');
  for (const p of problems) console.error(`  ✗ ${p}`);
  process.exit(1);
}
console.log(`repository sanity passed (${tracked.length} tracked files scanned)`);
