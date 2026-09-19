import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const results = ['TEST_RESULT', 'RACE_RESULT', 'WINDOWS_RESULT', 'INTEROP_RESULT'];
function check(full, overrides = {}) {
  const env = { ...process.env, CHANGES_RESULT: 'success', FULL: full,
    ...Object.fromEntries(results.map(key => [key, full === 'true' ? 'success' : 'skipped'])), ...overrides };
  return spawnSync('bash', [fileURLToPath(new URL('./ci-result.sh', import.meta.url))], {env, encoding: 'utf8'});
}

test('full and metadata-only success satisfy the required check', () => {
  for (const full of ['true', 'false']) assert.equal(check(full).status, 0);
});

test('classification failure, cancellation, skipping or missing outputs always block', () => {
  for (const full of ['true', 'false', '', 'unknown']) {
    for (const result of ['failure', 'cancelled', 'skipped', '']) {
      assert.notEqual(check(full, {CHANGES_RESULT: result}).status, 0);
    }
  }
  for (const full of ['', 'unknown']) assert.notEqual(check(full).status, 0);
});

test('every full-CI dependency must succeed and every unselected dependency must skip', () => {
  for (const key of results) {
    for (const full of ['true', 'false']) {
      for (const result of ['success', 'failure', 'cancelled', 'skipped', '']) {
        const expected = full === 'true' ? 'success' : 'skipped';
        assert.equal(check(full, {[key]: result}).status === 0, result === expected, `${full}: ${key}=${result}`);
      }
    }
  }
});
