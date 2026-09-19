import { execFileSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const releaseFiles = new Set(['.release-please-manifest.json', 'CHANGELOG.md']);
const rootDocs = new Set(['README.md', 'RELEASING.md', 'SECURITY.md', 'AGENTS.md', 'upstream/README.md']);
const stableVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const isDoc = path => rootDocs.has(path) || (path.startsWith('docs/') && path.endsWith('.md'));

// Classify the actual diff, never the author, label, title, or branch name.
// Unknown paths (including CI, fixtures, and schemas) always select full CI.
export function classifyPaths(paths) {
  return { full: paths.some(path => !isDoc(path) && !releaseFiles.has(path)) };
}

function gitAt(cwd) {
  return (...args) => execFileSync('git', args, { cwd, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 });
}

function manifestVersion(content) {
  const value = JSON.parse(content);
  if (!value || Array.isArray(value) || Object.keys(value).length !== 1 ||
      typeof value['.'] !== 'string' || !stableVersion.test(value['.'])) {
    throw new Error('release manifest must contain one root stable version');
  }
  return value['.'];
}

function changelogVersion(content) {
  const heading = content.split(/\r?\n/).find(line => line.startsWith('## '));
  const match = heading?.match(/^## (?:\[v?(\d+\.\d+\.\d+)\]|v?(\d+\.\d+\.\d+)(?=\s|$))/);
  return match?.[1] ?? match?.[2];
}

export function validateReleaseMetadata(cwd = process.cwd(), ref = 'HEAD') {
  const git = gitAt(cwd);
  for (const path of [...releaseFiles, 'README.md']) {
    if (!git('ls-tree', ref, '--', path).startsWith('100644 blob ')) {
      throw new Error(`${path} must be a regular non-executable file`);
    }
  }
  const version = manifestVersion(git('show', `${ref}:.release-please-manifest.json`));
  if (changelogVersion(git('show', `${ref}:CHANGELOG.md`)) !== version) {
    throw new Error(`first changelog release heading must match ${version}`);
  }
  const readme = git('show', `${ref}:README.md`);
  const installs = [...readme.matchAll(/^go get github\.com\/caelis-labs\/acp-go-sdk@v(\S+)$/gm)];
  if (installs.length !== 1 || installs[0][1] !== version) {
    throw new Error(`README installation version must match ${version}`);
  }
  return version;
}

export function inspectChanges(base, cwd = process.cwd()) {
  if (!/^[a-f0-9]{40}$/.test(base ?? '')) throw new Error('a full PR base SHA is required');
  const git = gitAt(cwd);
  git('merge-base', '--is-ancestor', base, 'HEAD');
  const paths = git('diff', '--no-ext-diff', '--no-textconv', '--no-renames', '--name-only', '-z', base, 'HEAD', '--')
    .split('\0').filter(Boolean);
  const scope = classifyPaths(paths);
  scope.release = false;
  // Consider both modes, including removed executables/symlinks at prose paths.
  for (const ref of [base, 'HEAD']) {
    const entries = new Map(git('ls-tree', '-r', '-z', ref).split('\0').filter(Boolean)
      .map(entry => [entry.slice(entry.indexOf('\t') + 1), entry.slice(0, 6)]));
    if (paths.some(path => isDoc(path) && entries.has(path) && entries.get(path) !== '100644')) {
      scope.full = true;
    }
  }
  const version = validateReleaseMetadata(cwd);
  if (paths.includes('.release-please-manifest.json')) {
    if (git('ls-tree', base, '--', '.release-please-manifest.json').trim() === '') {
      // Initial adoption records the already published version, not the next one.
      scope.full = true;
      if (changelogVersion(git('show', `${base}:CHANGELOG.md`)) !== version) {
        throw new Error('bootstrap manifest must match the previous changelog version');
      }
    } else {
      const previous = manifestVersion(git('show', `${base}:.release-please-manifest.json`));
      const before = previous.split('.').map(BigInt);
      const after = version.split('.').map(BigInt);
      const changed = after.findIndex((part, index) => part !== before[index]);
      if (changed < 0 || after[changed] < before[changed]) {
        throw new Error(`release version must increase from ${previous}, got ${version}`);
      }
      scope.release = true;
      scope.full = true;
    }
  }
  return scope;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const scope = inspectChanges(process.env.PR_BASE_SHA);
    console.log(`Full CI required: ${scope.full}`);
    appendFileSync(process.env.GITHUB_OUTPUT, `full=${scope.full}\nrelease=${scope.release}\n`);
  } catch (error) {
    console.error(`CI change inspection failed: ${error.message}`);
    process.exitCode = 1;
  }
}
