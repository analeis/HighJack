/**
 * Asset validation gate.
 *
 * Enforces the rules documented in assets/PROVENANCE.md:
 * - allowed file extensions
 * - kebab-case naming
 * - per-file size ceilings
 * - provenance manifest presence & coverage
 */
import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';

const ROOT = path.resolve(import.meta.dir, '..');

const ALLOWED_EXTENSIONS = new Set(['.svg', '.png', '.webp', '.avif', '.ico', '.md', '.json']);
const MAX_FILE_BYTES = 200 * 1024; // 200 KB ceiling for committed assets
const ASSET_DIRS = [
  'assets/source/branding',
  'assets/source/icons',
  'apps/web/public',
  'apps/game/public',
];

const problems: string[] = [];
let checked = 0;

function isKebabCase(name: string): boolean {
  return /^[a-z0-9]+(-[a-z0-9]+)*(\.[a-z0-9]+)+$/.test(name);
}

function walk(dir: string): void {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    problems.push(`missing asset directory: ${dir}`);
    return;
  }
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(full);
      continue;
    }
    checked++;
    const rel = path.relative(ROOT, full);
    if (!ALLOWED_EXTENSIONS.has(path.extname(entry.name))) {
      problems.push(`${rel}: extension ${path.extname(entry.name)} not allowed`);
    }
    if (!isKebabCase(entry.name)) {
      problems.push(`${rel}: filename must be kebab-case`);
    }
    const size = statSync(full).size;
    if (size > MAX_FILE_BYTES) {
      problems.push(
        `${rel}: ${(size / 1024).toFixed(1)} KB exceeds ${MAX_FILE_BYTES / 1024} KB ceiling (document an exception in assets/PROVENANCE.md or optimize)`,
      );
    }
  }
}

for (const dir of ASSET_DIRS) {
  walk(path.join(ROOT, dir));
}

// Provenance manifest must exist and reference the asset roots.
const provenancePath = path.join(ROOT, 'assets', 'PROVENANCE.md');
if (!statSync(provenancePath, { throwIfNoEntry: false })?.isFile()) {
  problems.push('assets/PROVENANCE.md is missing');
} else {
  const provenance = readFileSync(provenancePath, 'utf8');
  for (const dir of ['assets/source/branding', 'assets/source/icons']) {
    if (!provenance.includes(dir)) {
      problems.push(`assets/PROVENANCE.md does not mention ${dir}`);
    }
  }
  if (!provenance.toLowerCase().includes('ofl')) {
    problems.push('assets/PROVENANCE.md does not record font licensing (OFL)');
  }
}

console.log(`checked ${checked} asset files`);
if (problems.length > 0) {
  console.error('\nasset validation failed:');
  for (const p of problems) console.error(`  ✗ ${p}`);
  process.exit(1);
}
console.log('asset validation passed');
