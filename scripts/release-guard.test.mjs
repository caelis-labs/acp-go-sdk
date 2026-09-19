import assert from 'node:assert/strict';
import test from 'node:test';
import { releaseJobs, verifyReleaseEvidence } from './release-guard.mjs';

const base = 'a'.repeat(40), head = 'b'.repeat(40), merge = 'c'.repeat(40), tree = 'd'.repeat(40), target = 'e'.repeat(40);
function fixture() {
  const repository = 'owner/sdk';
  return {
    repository, version: '1.4.0',
    pr: {number: 8, merged: true, base: {ref: 'main'}, head: {sha: head, repo: {full_name: repository}}, merge_commit_sha: target},
    run: {id: 123, run_attempt: 2, status: 'completed', conclusion: 'success', path: '.github/workflows/ci.yml',
      event: 'pull_request', head_sha: head, head_repository: {full_name: repository}, pull_requests: [{number: 8, head: {sha: head}}]},
    jobs: releaseJobs.map(name => ({name, conclusion: 'success'})),
    approvals: [{state: 'approved', user: {login: 'maintainer', type: 'User'}, environments: [{name: 'release-validation'}]}],
    evidence: {format: 1, repository, run: 123, attempt: 2, pr: 8, full: true, release: true,
      mode: 'pull_request', head, base, commit: merge, tree, version: '1.4.0'},
    tested: {sha: merge, tree: {sha: tree}, parents: [{sha: base}, {sha: head}]},
    target: {sha: target, tree: {sha: tree}},
    interop: {status: 'passed', matrixComplete: true, dirty: false, goCommit: merge},
  };
}

test('squash commit may differ while the validated tree stays identical', () => verifyReleaseEvidence(fixture()));

test('explicit recovery validates the exact merged commit', () => {
  const f = fixture();
  Object.assign(f.run, {event: 'workflow_dispatch', head_branch: 'main', head_sha: 'f'.repeat(40)});
  Object.assign(f.evidence, {mode: 'workflow_dispatch', commit: target});
  Object.assign(f.tested, {sha: target, parents: [{sha: base}]});
  f.interop.goCommit = target;
  verifyReleaseEvidence(f);
});

for (const [name, mutate] of [
  ['unmerged PR', f => f.pr.merged = false],
  ['fork PR', f => f.pr.head.repo.full_name = 'fork/sdk'],
  ['wrong target branch', f => f.pr.base.ref = 'development'],
  ['foreign workflow', f => f.run.path = '.github/workflows/fake.yml'],
  ['foreign CI repository', f => f.run.head_repository.full_name = 'fork/sdk'],
  ['failed run', f => f.run.conclusion = 'failure'],
  ['incomplete run', f => f.run.status = 'in_progress'],
  ['missing approval', f => f.approvals = []],
  ['rejected approval', f => f.approvals[0].state = 'rejected'],
  ['different environment', f => f.approvals[0].environments[0].name = 'other'],
  ['bot approval', f => f.approvals[0].user.type = 'Bot'],
  ['old attempt', f => f.evidence.attempt = 1],
  ['wrong run', f => f.evidence.run++],
  ['wrong evidence repository', f => f.evidence.repository = 'fork/sdk'],
  ['wrong PR', f => f.evidence.pr++],
  ['ordinary validation', f => f.evidence.release = false],
  ['lightweight checks', f => f.evidence.full = false],
  ['old PR head', f => f.pr.head.sha = 'f'.repeat(40)],
  ['wrong version', f => f.version = '1.5.0'],
  ['changed merged tree', f => f.target.tree.sha = 'f'.repeat(40)],
  ['wrong merged commit', f => f.target.sha = 'f'.repeat(40)],
  ['forged tree', f => f.tested.tree.sha = 'f'.repeat(40)],
  ['wrong base parent', f => f.tested.parents[0].sha = 'f'.repeat(40)],
  ['wrong head parent', f => f.tested.parents[1].sha = 'f'.repeat(40)],
  ['unrelated run head', f => f.run.head_sha = 'f'.repeat(40)],
  ['unrelated PR run', f => f.run.pull_requests = []],
  ['unrecognized validation mode', f => f.evidence.mode = 'push'],
  ['missing interop cases', f => f.interop.matrixComplete = false],
  ['dirty conformance checkout', f => f.interop.dirty = true],
  ['wrong conformance commit', f => f.interop.goCommit = head],
]) {
  test(`refuse publication: ${name}`, () => {
    const f = fixture(); mutate(f); assert.throws(() => verifyReleaseEvidence(f));
  });
}
for (const name of releaseJobs) {
  for (const conclusion of ['skipped', 'failure', 'cancelled', undefined]) {
    test(`refuse ${name}=${conclusion}`, () => {
      const f = fixture(); f.jobs.find(job => job.name === name).conclusion = conclusion;
      assert.throws(() => verifyReleaseEvidence(f));
    });
  }
}
test('missing and duplicate job results fail closed', () => {
  const f = fixture(); f.jobs.pop(); assert.throws(() => verifyReleaseEvidence(f));
  const duplicate = fixture(); duplicate.jobs.push(duplicate.jobs[0]); assert.throws(() => verifyReleaseEvidence(duplicate));
});
