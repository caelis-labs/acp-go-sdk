import { execFileSync } from 'node:child_process';
import { appendFileSync, readFileSync } from 'node:fs';
import { inspectChanges, validateReleaseMetadata } from './ci-scope.mjs';

const git = (...args) => execFileSync('git', args, {encoding: 'utf8', timeout: 60000}).trim();
const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
const repo = process.env.GITHUB_REPOSITORY;
let pr = event.pull_request;
let commit = process.env.GITHUB_SHA;
let base = pr?.base.sha;
const mode = process.env.GITHUB_EVENT_NAME;
if (mode === 'workflow_dispatch') {
  if (process.env.GITHUB_REF !== 'refs/heads/main' || !/^[1-9]\d*$/.test(event.inputs.release_pr ?? '')) {
    throw new Error('recovery validation must be dispatched on main for a merged release PR');
  }
  pr = JSON.parse(execFileSync('gh', ['api', `repos/${repo}/pulls/${event.inputs.release_pr}`], {encoding: 'utf8', timeout: 30000}));
  if (!pr.merged || pr.base.ref !== 'main' || pr.head.repo?.full_name !== repo) {
    throw new Error('recovery requires a merged same-repository PR targeting main');
  }
  commit = pr.merge_commit_sha;
  if (!/^[a-f0-9]{40}$/.test(commit)) throw new Error('invalid merged commit');
  git('merge-base', '--is-ancestor', commit, 'origin/main');
  git('checkout', '--detach', commit);
  base = git('rev-parse', 'HEAD^');
} else if (mode === 'pull_request') {
  if (git('rev-parse', 'HEAD') !== commit || git('rev-parse', 'HEAD^1') !== base || git('rev-parse', 'HEAD^2') !== pr.head.sha) {
    throw new Error('checkout does not match the event PR merge candidate');
  }
} else {
  throw new Error('unsupported CI event');
}
const scope = inspectChanges(base);
if (mode === 'workflow_dispatch' && !scope.release) throw new Error('recovery commit must increment the release version');
const context = {
  ...scope, mode, pr: pr.number, head: pr.head.sha, base, commit,
  tree: git('rev-parse', 'HEAD^{tree}'), version: validateReleaseMetadata(),
};
console.log(JSON.stringify(context));
appendFileSync(process.env.GITHUB_OUTPUT, `full=${scope.full}\nrelease=${scope.release}\ncommit=${commit}\ncontext=${JSON.stringify(context)}\n`);
