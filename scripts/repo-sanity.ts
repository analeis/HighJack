/**
 * Repository sanity gate.
 *
 * Structural checks that keep the repository honest:
 * - required structure exists
 * - no committed secrets / env files
 * - file size ceilings (catches stray binaries)
 * - documentation links resolve
 * - no build outputs or junk tracked in git
 * - CI enforces every gate the pipeline defines
 */
import { execSync } from 'node:child_process';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { validationGates } from './lib/validation-gates.ts';
import { verificationGates } from './lib/verification-gates.ts';

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
const tracked = [...candidates].toSorted();
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

// ---- CI / pipeline agreement ---------------------------------------------------

// Every gate the pipeline defines must also be enforced by CI.
//
// This exists because the two drifted and the drift was invisible. CI's database
// step once carried a `-run` filter that silently excluded the JSONB round-trip and
// the interrupted-match policy tests while the step reported success. The gates are
// now written out in the workflow using the toolchain each job already has — the
// backend job runs `go` directly rather than through a JavaScript runner — so the
// commands are no longer literally identical to the pipeline's. That makes a
// comparison the thing that keeps them honest.
//
// The comparison is on substance, not spelling. A gate counts as enforced when the
// workflow invokes it by name (`bun scripts/gate.ts <name>`) or contains its
// command. Two differences are normalised away first, because neither changes what
// is checked:
//   - output paths: certification builds to a temp directory so it cannot dirty the
//     tree it is certifying, while CI builds to ./bin. Same check, different path.
//   - shell control operators: a gate may report success with `&& echo`, while CI
//     prefers `|| { ...; exit 1; }` so a failure prints its cause. The check is the
//     part before the first operator.
function normalise(text: string): string {
  return text
    .replace(/\s+/g, ' ')
    .replace(/(^|\s)-o\s+\S+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

const workflowPath = path.join(ROOT, '.github', 'workflows', 'ci.yml');
if (existsSync(workflowPath)) {
  // Collapse newlines and indentation so a wrapped YAML block still matches a
  // one-line command.
  const flat = normalise(readFileSync(workflowPath, 'utf8'));

  for (const gate of [...validationGates, ...verificationGates]) {
    const invokedByName = flat.includes(`gate.ts ${gate.name}`);
    const first = gate.cmd[0] ?? '';
    const isShell = first === 'bash' || first === 'sh';
    // For a shell gate the interesting text is inside the script, not the binary.
    const raw = isShell ? (gate.cmd[2] ?? '') : gate.cmd.join(' ');
    const command = normalise(raw.split(/&&|\|\||;/)[0] ?? '');
    const present = invokedByName || (command.length > 0 && flat.includes(command));
    if (!present) {
      problems.push(
        `gate "${gate.name}" is defined in the pipeline but not enforced by CI ` +
          `(looked for: ${command || first})`,
      );
    }
  }
}

// ---- the protocol document must describe the protocol ------------------------

// The protocol overview described the v0.1 wire protocol: it listed four
// server→client frames and omitted the four the server has actually sent
// since v0.2.0, and it presented the pre-v0.2.2 per-event frame as current.
// Nothing caught it, because prose is not checked.
//
// The frame list is read from the shared TypeScript contract, which the Go
// parity suite already holds to the Go implementation. The document is then
// held to the contract, so a frame cannot be added to the wire without being
// documented, and cannot be documented without existing.
const protocolDocPath = path.join(ROOT, 'docs/protocol/OVERVIEW.md');
const messagesPath = path.join(ROOT, 'packages/protocol/src/messages.ts');
if (existsSync(protocolDocPath) && existsSync(messagesPath)) {
  const messages = readFileSync(messagesPath, 'utf8');
  const doc = readFileSync(protocolDocPath, 'utf8');

  // The server→client frame types, from the ServerMessage union.
  const union = messages.match(/export type ServerMessage =([\s\S]*?);/);
  const frameTypes = union
    ? [...(union[1] ?? '').matchAll(/(\w+Message)\b/g)].map((m) => m[1] ?? '')
    : [];
  if (frameTypes.length === 0) {
    problems.push('could not read the ServerMessage union from packages/protocol/src/messages.ts');
  }
  for (const name of frameTypes) {
    const type = name.replace(/Message$/, '').toLowerCase();
    // A frame counts as described if it appears as a wire example or as a
    // named frame in prose. The legacy per-event frame is documented in prose
    // rather than as an example, because the server no longer sends it.
    const described = doc.includes(`"type": "${type}"`) || doc.includes('`' + type + '`');
    if (!described) {
      problems.push(
        `the protocol document does not describe the "${type}" frame; the shared contract ` +
          'declares it, so the wire and the document have drifted',
      );
    }
  }
}

// ---- the toolchain the gates need --------------------------------------------

// The typecheck gate runs `bun check`, which was added in Bun 1.4.3.
//
// The repository used to pin `bun@1.3.14` in `packageManager`, and CI pinned
// the same version. The gate therefore failed in CI with "command not found"
// while the identical gate passed locally on 1.4.3 — a green local run that
// proved nothing about the release, which is the exact failure mode this
// project has spent a release cycle fighting.
//
// These checks make that impossible to reintroduce silently: the requirement is
// declared once, in `engines.bun`, and the workflow may not pin anything below
// it.
const MINIMUM_BUN = '1.4.3';

function compareVersions(a: string, b: string): number {
  const pa = a.split('.').map((n) => Number(n) || 0);
  const pb = b.split('.').map((n) => Number(n) || 0);
  for (let i = 0; i < 3; i++) {
    const diff = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (diff !== 0) return Math.sign(diff);
  }
  return 0;
}

const rootManifestPath = path.join(ROOT, 'package.json');
if (existsSync(rootManifestPath)) {
  const manifest = JSON.parse(readFileSync(rootManifestPath, 'utf8')) as {
    packageManager?: string;
    engines?: { bun?: string };
  };

  // A `packageManager` field is a Corepack pin, and Corepack does not manage
  // Bun. It is read by other tools, and a stale value there is worse than
  // none: it names a version that cannot run the project's own gates.
  if (manifest.packageManager) {
    const pinned = manifest.packageManager.replace(/^bun@/, '');
    if (compareVersions(pinned, MINIMUM_BUN) < 0) {
      problems.push(
        `packageManager pins bun ${pinned}, but the gates need ${MINIMUM_BUN} or later ` +
          '(`bun check` was added in 1.4.3). State the minimum in engines.bun instead.',
      );
    }
  }

  const required = manifest.engines?.bun;
  if (!required) {
    problems.push(
      `engines.bun is missing: the typecheck gate needs Bun ${MINIMUM_BUN} or later, and the ` +
        'requirement has to be stated somewhere the tools read',
    );
  } else if (!/\d+\.\d+\.\d+/.test(required)) {
    problems.push(`engines.bun is ${JSON.stringify(required)}, which names no version`);
  } else {
    const floor = required.match(/\d+\.\d+\.\d+/)?.[0] ?? '0.0.0';
    if (compareVersions(floor, MINIMUM_BUN) < 0) {
      problems.push(
        `engines.bun allows ${floor}, but the typecheck gate needs ${MINIMUM_BUN} or later`,
      );
    }
  }
}

// The workflow may not pin a Bun that lacks a command a gate uses.
if (existsSync(workflowPath)) {
  const workflow = readFileSync(workflowPath, 'utf8');
  for (const m of workflow.matchAll(/bun-version:\s*['"]?([^'"\s]+)['"]?/g)) {
    const pinned = (m[1] ?? '').trim();
    if (pinned === '' || pinned === 'latest' || pinned === 'nightly') continue;
    if (compareVersions(pinned, MINIMUM_BUN) < 0) {
      problems.push(
        `the workflow pins bun ${pinned}, but the typecheck gate needs ${MINIMUM_BUN} or later ` +
          "(`bun check` was added in 1.4.3). Use 'latest' or a version at or above it.",
      );
    }
  }
}

// ---- release version ---------------------------------------------------------

// The repository's VERSION file is the single source of truth for which
// release this is. Everything that states a version reads it: the workspace
// manifests, the game client's build, the server binary's /version endpoint,
// and the tag the release is cut from.
//
// These checks exist because the versions did not agree. Every manifest said
// 0.1.0, the game client's badge said v0.2.0, the tags were at v0.2.4, and the
// server binary reported "dev" and "unknown" because no build path ever passed
// it a version at all. A reader could not tell which release they were looking
// at, and the /version endpoint could not identify a running build.
const versionPath = path.join(ROOT, 'VERSION');
const releaseVersion = existsSync(versionPath) ? readFileSync(versionPath, 'utf8').trim() : '';

if (!/^\d+\.\d+\.\d+$/.test(releaseVersion)) {
  problems.push(
    `VERSION must be a bare semver like 0.2.7, found ${releaseVersion ? `"${releaseVersion}"` : '(no VERSION file)'}`,
  );
} else {
  // The manifests.
  const manifests = [
    'package.json',
    'apps/game/package.json',
    'apps/web/package.json',
    'packages/protocol/package.json',
    'packages/ui/package.json',
  ];
  for (const rel of manifests) {
    const abs = path.join(ROOT, rel);
    if (!existsSync(abs)) continue;
    try {
      const declared = JSON.parse(readFileSync(abs, 'utf8')).version as string | undefined;
      if (declared !== releaseVersion) {
        problems.push(
          `${rel} declares version ${declared ?? '(none)'}, but VERSION says ${releaseVersion}`,
        );
      }
    } catch (err) {
      problems.push(`${rel} is not readable JSON: ${(err as Error).message}`);
    }
  }

  // The game client's badge. It used to be a literal, and it was two releases
  // stale. It now reads __APP_VERSION__, which the build injects from VERSION.
  //
  // The whole client source is searched rather than one file: the badge moved
  // into a component of its own when the client was split up, and a check pinned
  // to a filename would have failed for the wrong reason.
  const clientSrc = path.join(ROOT, 'apps/game/src');
  if (existsSync(clientSrc)) {
    let usesVersion = false;
    for (const file of readdirSync(clientSrc, { recursive: true })) {
      // A `.d.ts` only declares the constant; it does not use it. Counting the
      // declaration would let the check pass while the badge had gone back to
      // a literal.
      if (typeof file !== 'string' || !/\.(ts|tsx)$/.test(file) || file.endsWith('.d.ts')) {
        continue;
      }
      if (readFileSync(path.join(clientSrc, file), 'utf8').includes('__APP_VERSION__')) {
        usesVersion = true;
        break;
      }
    }
    if (!usesVersion) {
      problems.push(
        'the game client does not reference __APP_VERSION__; it must read its version from ' +
          'the VERSION file rather than a literal',
      );
    }
  }

  // The tag. A release commit is certified *before* it is tagged, so the
  // nearest reachable tag is normally the previous release and the two are
  // expected to differ. What must never happen is the tag being *ahead* of
  // VERSION, which would mean the version was rolled back below a release
  // that already exists.
  try {
    const tag = execSync('git describe --tags --abbrev=0', {
      cwd: ROOT,
      encoding: 'utf8',
    }).trim();
    const tagged = tag.replace(/^v/, '');
    if (/^\d+\.\d+\.\d+$/.test(tagged) && compareSemver(tagged, releaseVersion) > 0) {
      problems.push(
        `the nearest tag is ${tag}, which is newer than VERSION (${releaseVersion}): ` +
          'the release version was rolled back below a tag that already exists',
      );
    }
  } catch {
    // No tag reachable: this repository has never been released. Nothing to
    // check.
  }
}

/**
 * Compares two bare semver strings. A pre-release or build suffix sorts before
 * the plain version, which is the usual semver rule.
 */
function compareSemver(a: string, b: string): number {
  const parse = (v: string): { nums: number[]; pre: string } => {
    const parts = v.split('-', 2);
    const core = parts[0] ?? '';
    const pre = parts[1] ?? '';
    return { nums: core.split('.').map((n) => Number(n) || 0), pre };
  };
  const pa = parse(a);
  const pb = parse(b);
  for (let i = 0; i < 3; i++) {
    const diff = (pa.nums[i] ?? 0) - (pb.nums[i] ?? 0);
    if (diff !== 0) return Math.sign(diff);
  }
  if (pa.pre === pb.pre) return 0;
  if (pa.pre === '') return 1;
  if (pb.pre === '') return -1;
  return pa.pre < pb.pre ? -1 : 1;
}

// ---- report -------------------------------------------------------------------

if (problems.length > 0) {
  console.error('repository sanity check failed:');
  for (const p of problems) console.error(`  ✗ ${p}`);
  process.exit(1);
}
console.log(`repository sanity passed (${tracked.length} tracked files scanned)`);
