export const releaseJobs = ['changes', 'release-approval', 'test (1.23.x)', 'test (1.25.x)',
  'race-and-static', 'windows-stdio', 'official-sdk-interop', 'release-acceptance', 'quality'];

export function verifyReleaseEvidence({repository, pr, run, jobs, evidence, tested, target, version, approvals, interop}) {
  const check = (ok, message) => { if (!ok) throw new Error(message); };
  const sha = value => typeof value === 'string' && /^[a-f0-9]{40}$/.test(value);
  check(pr.merged && pr.base.ref === 'main' && pr.head.repo?.full_name === repository, 'release PR must be merged into main from this repository');
  check(run.path === '.github/workflows/ci.yml' && run.status === 'completed' && run.conclusion === 'success' &&
    run.head_repository?.full_name === repository, 'release requires successful repository CI');
  for (const name of releaseJobs) {
    const matches = jobs.filter(job => job.name === name);
    check(matches.length === 1 && matches[0].conclusion === 'success', `release check ${name} did not succeed`);
  }
  check(approvals.some(review => review.state === 'approved' && review.user?.type === 'User' &&
    review.environments?.some(env => env.name === 'release-validation')), 'missing maintainer environment approval');
  check(evidence.format === 1 && evidence.repository === repository && evidence.run === run.id &&
    evidence.attempt === run.run_attempt && evidence.pr === pr.number && evidence.full === true && evidence.release === true,
  'validation provenance mismatch');
  check(evidence.head === pr.head.sha && evidence.version === version && sha(evidence.head) &&
    sha(evidence.base) && sha(evidence.commit) && sha(evidence.tree), 'candidate identity mismatch');
  check(tested.sha === evidence.commit && tested.tree.sha === evidence.tree && target.sha === pr.merge_commit_sha &&
    target.tree.sha === evidence.tree, 'published tree differs from the tested candidate; revalidate the merged release PR');
  if (evidence.mode === 'pull_request') {
    check(run.event === 'pull_request' && run.head_sha === evidence.head &&
      run.pull_requests.some(item => item.number === pr.number && item.head.sha === evidence.head), 'CI does not belong to this PR head');
    check(tested.parents.length === 2 && tested.parents[0].sha === evidence.base && tested.parents[1].sha === evidence.head,
      'CI did not validate the claimed PR merge candidate');
  } else {
    check(evidence.mode === 'workflow_dispatch' && run.event === 'workflow_dispatch' && run.head_branch === 'main' &&
      evidence.commit === pr.merge_commit_sha && tested.parents[0].sha === evidence.base, 'invalid merged-release recovery validation');
  }
  check(interop.status === 'passed' && interop.matrixComplete === true && interop.dirty === false &&
    interop.goCommit === evidence.commit, 'missing clean cross-SDK conformance evidence for the tested commit');
}
