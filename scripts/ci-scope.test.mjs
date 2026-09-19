import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, renameSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { classifyPaths, inspectChanges, validateReleaseMetadata } from './ci-scope.mjs';

function fixture(t, bootstrap = false) {
  const cwd = mkdtempSync(join(tmpdir(), 'acp-ci-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const git = (...args) => execFileSync('git', ['-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=/dev/null', ...args], {
    cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'],
  }).trim();
  const write = (path, content) => {
    mkdirSync(dirname(join(cwd, path)), { recursive: true });
    writeFileSync(join(cwd, path), content);
  };
  const commit = () => { git('add', '-A'); git('commit', '--allow-empty', '-m', 'fixture'); return git('rev-parse', 'HEAD'); };
  const release = (version = '1.4.0') => {
    write('.release-please-manifest.json', JSON.stringify({'.': version}));
    write('CHANGELOG.md', `# Changelog\n\n## [v${version}] - 2026-09-19\n\nRelease notes\n`);
    write('README.md', `# SDK\n\ngo get github.com/caelis-labs/acp-go-sdk@v${version}\n`);
  };
  git('init', '-b', 'main');
  git('config', 'user.name', 'CI Test');
  git('config', 'user.email', 'ci@example.invalid');
  release('1.3.0');
  if (bootstrap) rmSync(join(cwd, '.release-please-manifest.json'));
  write('source.go', 'package example\n');
  return { cwd, git, write, commit, release, base: commit() };
}

test('only maintained prose and release metadata can select lightweight CI', () => {
  for (const path of ['README.md', 'RELEASING.md', 'SECURITY.md', 'AGENTS.md', 'upstream/README.md',
    'docs/upgrading-to-v1.4.0.md', 'CHANGELOG.md', '.release-please-manifest.json']) {
    assert.equal(classifyPaths([path]).full, false, path);
  }
  for (const path of ['go.mod', 'go.sum', 'Makefile', '.github/workflows/ci.yml', 'release-please-config.json',
    'scripts/ci-scope.mjs', 'types_gen.go', 'experimental/v2/types_gen.go', 'schema/lock.json', 'upstream/lock.json',
    'interop/peers/rust/Cargo.lock', 'testdata/frame.json', 'docs/example.go', 'unknown.md', 'README.md\nsource.go']) {
    assert.equal(classifyPaths([path]).full, true, path);
  }
  assert.equal(classifyPaths(['README.md', 'source.go']).full, true);
});

test('a real release diff validates metadata without selecting full checks', t => {
  const f = fixture(t);
  f.release(); f.commit();
  assert.deepEqual(inspectChanges(f.base, f.cwd), {full: false});
  assert.equal(validateReleaseMetadata(f.cwd), '1.4.0');
  for (const heading of ['## [1.4.0](https://example.invalid/compare) (2026-09-19)', '## 1.4.0 (2026-09-19)']) {
    f.write('CHANGELOG.md', `# Changelog\n\n${heading}\nDetails\n`); f.commit();
    assert.equal(inspectChanges(f.base, f.cwd).full, false);
  }
  f.write('source.go', 'package updated\n'); f.commit();
  assert.equal(inspectChanges(f.base, f.cwd).full, true, 'release metadata cannot hide a code change');
});

test('changelog prose can be corrected without incrementing the manifest', t => {
  const f = fixture(t);
  f.write('CHANGELOG.md', '# Changelog\n\n## [v1.3.0] - 2026-09-11\nCorrected notes\n'); f.commit();
  assert.equal(inspectChanges(f.base, f.cwd).full, false);
});

for (const [name, mutate, error] of [
  ['malformed JSON', f => f.write('.release-please-manifest.json', '{broken'), /JSON/],
  ['null manifest', f => f.write('.release-please-manifest.json', 'null'), /one root/],
  ['extra component', f => f.write('.release-please-manifest.json', '{".":"1.4.0","other":"1.4.0"}'), /one root/],
  ['prerelease', f => f.release('1.4.0-rc.1'), /stable version/],
  ['unchanged version', f => { f.release('1.3.0'); f.write('.release-please-manifest.json', '{ ".": "1.3.0" }'); }, /must increase/],
  ['decreased version', f => f.release('1.2.9'), /must increase/],
  ['mismatched heading', f => f.write('CHANGELOG.md', '# Changelog\n\n## 1.5.0\n'), /heading must match/],
  ['unreleased heading', f => f.write('CHANGELOG.md', '# Changelog\n\n## Unreleased\n\n## 1.4.0\n'), /heading must match/],
  ['mismatched README', f => f.write('README.md', 'go get github.com/caelis-labs/acp-go-sdk@v1.3.0\n'), /installation version/],
  ['missing changelog', f => rmSync(join(f.cwd, 'CHANGELOG.md')), /regular non-executable/],
  ['deleted manifest', f => rmSync(join(f.cwd, '.release-please-manifest.json')), /regular non-executable/],
  ['executable metadata', f => chmodSync(join(f.cwd, 'CHANGELOG.md'), 0o755), /regular non-executable/],
  ['symlink metadata', f => { rmSync(join(f.cwd, 'CHANGELOG.md')); symlinkSync('source.go', join(f.cwd, 'CHANGELOG.md')); }, /regular non-executable/],
]) {
  test(`reject ${name}`, t => {
    const f = fixture(t); f.release(); mutate(f); f.commit();
    assert.throws(() => inspectChanges(f.base, f.cwd), error);
  });
}

test('bootstrap records the last published version and always selects full CI', t => {
  const f = fixture(t, true);
  f.release('1.3.0'); f.commit();
  assert.equal(inspectChanges(f.base, f.cwd).full, true);
  f.release('1.4.0'); f.commit();
  assert.throws(() => inspectChanges(f.base, f.cwd), /bootstrap manifest/);
});

test('rename or delete source still selects full checks', t => {
  const f = fixture(t);
  renameSync(join(f.cwd, 'source.go'), join(f.cwd, 'RELEASING.md')); f.commit();
  assert.equal(inspectChanges(f.base, f.cwd).full, true);
});

for (const mode of ['executable', 'symlink']) {
  test(`${mode} prose and its later removal select full checks`, t => {
    const f = fixture(t);
    if (mode === 'symlink') symlinkSync('source.go', join(f.cwd, 'RELEASING.md'));
    else { f.write('RELEASING.md', 'text'); chmodSync(join(f.cwd, 'RELEASING.md'), 0o755); }
    const abnormal = f.commit();
    assert.equal(inspectChanges(f.base, f.cwd).full, true);
    rmSync(join(f.cwd, 'RELEASING.md')); f.commit();
    assert.equal(inspectChanges(abnormal, f.cwd).full, true);
  });
}

test('shallow merge diff excludes changes already on main', t => {
  const f = fixture(t);
  f.git('checkout', '-b', 'pr'); f.write('docs/testing.md', '# Testing\n'); f.commit();
  f.git('checkout', 'main'); f.write('source.go', 'package changed\n'); const currentBase = f.commit();
  f.git('merge', '--no-ff', 'pr', '-m', 'PR merge');
  const shallow = join(f.cwd, 'shallow');
  f.git('clone', '--depth=2', `file://${f.cwd}`, shallow);
  assert.equal(inspectChanges(currentBase, shallow).full, false);
  assert.equal(inspectChanges(f.base, f.cwd).full, true);
});

test('unavailable or unrelated base cannot emit a successful scope', t => {
  const f = fixture(t);
  f.git('checkout', '--orphan', 'unrelated'); f.write('unrelated', 'text'); const unrelated = f.commit();
  f.git('checkout', 'main');
  for (const base of ['', 'HEAD', '0'.repeat(40), unrelated]) {
    const output = join(f.cwd, 'output');
    const result = spawnSync(process.execPath, [fileURLToPath(new URL('./ci-scope.mjs', import.meta.url))], {
      cwd: f.cwd, env: {...process.env, PR_BASE_SHA: base, GITHUB_OUTPUT: output}, encoding: 'utf8',
    });
    assert.notEqual(result.status, 0, result.stdout);
    assert.equal(existsSync(output), false);
  }
});
