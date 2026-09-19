import { execFileSync } from 'node:child_process';
import { appendFileSync, copyFileSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { validateReleaseMetadata } from './ci-scope.mjs';
import { verifyReleaseEvidence } from './release-guard.mjs';
import { unexpectedBreakingChanges } from './release-api-check.mjs';

const repo = process.env.GITHUB_REPOSITORY;
if (!/^[\w.-]+\/[\w.-]+$/.test(repo ?? '')) throw new Error('invalid repository');
const root = `repos/${repo}`;
const runCommand = (command, args, input) => execFileSync(command, args, {
  encoding: 'utf8', input, timeout: 120000, maxBuffer: 16 * 1024 * 1024,
  stdio: ['pipe', 'pipe', 'pipe'],
}).trim();
const gh = (...args) => runCommand('gh', args);
const git = (...args) => runCommand('git', args);
function api(path, body) {
  const args = ['api', `${root}/${path}`];
  if (body !== undefined) args.push('--method', 'POST', '--input', '-');
  const value = runCommand('gh', args, body === undefined ? undefined : JSON.stringify(body));
  return value ? JSON.parse(value) : null;
}
function optional(path) {
  try { return api(path); } catch (error) {
    if (String(error.stderr).includes('(HTTP 404)')) return null;
    throw error;
  }
}
function output(key, value) { appendFileSync(process.env.GITHUB_OUTPUT, `${key}=${value}\n`); }
function positiveID(value) {
  if (!/^[1-9]\d*$/.test(String(value)) || !Number.isSafeInteger(Number(value))) throw new Error('invalid PR or run ID');
  return Number(value);
}
function versionAt(ref) { return validateReleaseMetadata(process.cwd(), ref); }
function isVersionBump(commit) {
  if (!git('ls-tree', `${commit}^`, '--', '.release-please-manifest.json')) return false;
  const previous = JSON.parse(git('show', `${commit}^:.release-please-manifest.json`))['.'];
  const next = versionAt(commit);
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(previous)) throw new Error('invalid previous version');
  const before = previous.split('.').map(BigInt), after = next.split('.').map(BigInt);
  const change = after.findIndex((part, i) => part !== before[i]);
  if (change >= 0 && after[change] < before[change]) throw new Error('release version decreased');
  return change >= 0;
}
function download(run, name, directory) {
  rmSync(directory, {recursive: true, force: true});
  mkdirSync(directory, {recursive: true});
  gh('run', 'download', String(run.id), '--repo', repo, '--name', name, '--dir', directory);
}
async function prepare() {
  const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
  let pr, requestedRun;
  if (process.env.GITHUB_EVENT_NAME === 'push') {
    const commit = process.env.GITHUB_SHA;
    if (git('rev-parse', 'HEAD') !== commit) throw new Error('checkout does not match push');
    if (!isVersionBump(commit)) { output('publish', 'false'); return; }
    const prs = api(`commits/${commit}/pulls?per_page=100`).filter(item => item.merged_at && item.merge_commit_sha === commit && item.base.ref === 'main');
    if (prs.length !== 1) throw new Error('version bump must come from one merged release PR');
    pr = api(`pulls/${prs[0].number}`);
  } else if (process.env.GITHUB_EVENT_NAME === 'workflow_dispatch' && process.env.GITHUB_REF === 'refs/heads/main') {
    pr = api(`pulls/${positiveID(event.inputs.release_pr)}`);
    requestedRun = positiveID(event.inputs.validation_run);
  } else { throw new Error('publication must run on main'); }
  if (!pr.merged || pr.base.ref !== 'main' || pr.head.repo?.full_name !== repo || !/^[a-f0-9]{40}$/.test(pr.merge_commit_sha)) {
    throw new Error('expected a merged same-repository release PR');
  }
  const commit = pr.merge_commit_sha;
  git('merge-base', '--is-ancestor', commit, 'origin/main');
  if (!isVersionBump(commit)) throw new Error('PR does not increment the version');
  const version = versionAt(commit);
  const candidates = requestedRun ? [api(`actions/runs/${requestedRun}`)] :
    api(`actions/workflows/ci.yml/runs?event=pull_request&head_sha=${pr.head.sha}&status=success&per_page=10`).workflow_runs;
  const target = api(`git/commits/${commit}`);
  const errors = [];
  for (const candidate of candidates) {
    try {
      const run = api(`actions/runs/${candidate.id}`);
      const directory = resolve('.artifacts/publication');
      download(run, `release-validation-${run.run_attempt}`, `${directory}/validation`);
      const evidence = JSON.parse(readFileSync(`${directory}/validation/release-validation.json`, 'utf8'));
      if (!/^[a-f0-9]{40}$/.test(evidence.commit)) throw new Error('invalid evidence commit');
      const tested = api(`git/commits/${evidence.commit}`);
      const jobs = api(`actions/runs/${run.id}/attempts/${run.run_attempt}/jobs?per_page=100`);
      if (jobs.total_count !== jobs.jobs.length) throw new Error('unexpected paginated CI job list');
      const approvals = api(`actions/runs/${run.id}/approvals`);
      download(run, `official-sdk-interop-evidence-${run.run_attempt}`, `${directory}/interop`);
      const interopPath = `${directory}/interop/.artifacts/interop/evidence.json`;
      const interop = JSON.parse(readFileSync(interopPath, 'utf8'));
      verifyReleaseEvidence({repository: repo, pr, run, jobs: jobs.jobs, evidence, tested, target, version, approvals, interop});
      const approved = approvals.filter(review => review.state === 'approved' && review.user?.type === 'User' &&
        review.environments.some(env => env.name === 'release-validation'));
      const maintainer = approved.find(review => ['admin', 'maintain', 'write'].includes(
        api(`collaborators/${encodeURIComponent(review.user.login)}/permission`).permission));
      if (!maintainer) throw new Error('approval must come from a repository maintainer');
      download(run, `release-acceptance-${run.run_attempt}`, `${directory}/acceptance`);
      const apiReport = readFileSync(`${directory}/acceptance/api-diff.txt`, 'utf8');
      if (unexpectedBreakingChanges(apiReport).length) throw new Error('unexpected stable API changes');
      copyFileSync(interopPath, `${directory}/interop-evidence.json`);
      copyFileSync(`${directory}/validation/release-validation.json`, `${directory}/release-validation.json`);
      copyFileSync(`${directory}/acceptance/api-diff.txt`, `${directory}/api-diff.txt`);
      const changelog = git('show', `${commit}:CHANGELOG.md`);
      const headings = [...changelog.matchAll(/^## /gm)];
      const notes = changelog.slice(headings[0].index, headings[1]?.index).trim();
      const body = `${notes}\n\nValidated by [complete release CI](${run.html_url}), approved by @${maintainer.user.login}.\n\nRelease commit: \`${commit}\`\nTested tree: \`${evidence.tree}\`\n`;
      writeFileSync(`${directory}/notes.md`, body);
      writeFileSync(`${directory}/plan.json`, JSON.stringify({repository: repo, pr: pr.number, version, commit, tree: evidence.tree, run: run.id}, null, 2));
      output('publish', 'true');
      console.log(`Validated v${version} at ${commit} against ${run.html_url}`);
      return;
    } catch (error) { errors.push(`run ${candidate.id}: ${error.message}`); }
  }
  throw new Error(`No approved complete CI matches the release tree. Revalidate the merged release PR via CI workflow_dispatch, then retry publication.\n${errors.join('\n')}`);
}
function publish() {
  const directory = resolve('.artifacts/publication');
  const plan = JSON.parse(readFileSync(`${directory}/plan.json`, 'utf8'));
  if (plan.repository !== repo || !/^[a-f0-9]{40}$/.test(plan.commit) || !/^[1-9]\d*\.\d+\.\d+$/.test(plan.version)) throw new Error('invalid publication plan');
  const tag = `v${plan.version}`;
  const ref = optional(`git/ref/tags/${tag}`);
  if (ref) {
    let object = ref.object;
    for (let i = 0; object.type === 'tag' && i < 5; i++) object = api(`git/tags/${object.sha}`).object;
    if (object.type !== 'commit' || object.sha !== plan.commit) throw new Error('existing tag points elsewhere; never move it');
  } else {
    const object = api('git/tags', {tag, message: `${tag}\n\nValidated by https://github.com/${repo}/actions/runs/${plan.run}`, object: plan.commit, type: 'commit'});
    api('git/refs', {ref: `refs/tags/${tag}`, sha: object.sha});
  }
  let release = optional(`releases/tags/${tag}`);
  if (!release) {
    gh('release', 'create', tag, '--repo', repo, '--verify-tag', '--draft', '--title', `ACP Go SDK ${tag}`, '--notes-file', `${directory}/notes.md`);
    release = api(`releases/tags/${tag}`);
  }
  if (release.draft) {
    gh('release', 'upload', tag, '--repo', repo, '--clobber', `${directory}/release-validation.json`, `${directory}/api-diff.txt`, `${directory}/interop-evidence.json`);
    gh('release', 'edit', tag, '--repo', repo, '--draft=false', '--latest', '--notes-file', `${directory}/notes.md`);
  } else if (release.prerelease) { throw new Error('existing release is a prerelease'); }
  gh('label', 'create', 'autorelease: tagged', '--repo', repo, '--color', 'ededed', '--description', 'Release has been published', '--force');
  gh('pr', 'edit', String(plan.pr), '--repo', repo, '--add-label', 'autorelease: tagged', '--remove-label', 'autorelease: pending');
  console.log(`Published https://github.com/${repo}/releases/tag/${tag}`);
}
if (process.argv[2] === 'prepare') await prepare();
else if (process.argv[2] === 'publish') publish();
else throw new Error('usage: publish-release.mjs prepare|publish');
