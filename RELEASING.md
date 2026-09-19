# Releasing ACP Go SDK

ACP Go SDK versions are immutable. Never move, delete, or reuse a public tag:
GitHub, Go proxies, checksum databases, and downstream caches may retain it.

## Development and release validation

Ordinary PRs do not have to follow every movement of `main`. Code, protocol,
schema, dependency, workflow, and unknown-path changes still run the Go version
matrix, race/static checks, native Windows stdio, and official TypeScript/Rust
interoperability. Documentation-only changes use lightweight checks. Full CI
runs on PRs, not again on every main push.

A release is a separate acceptance decision:

1. Use Conventional Commit PR titles and squash merges. `feat:` selects a minor
   release, `fix:` selects a patch; `docs:` and `chore:` normally do not start a
   release. A stable API breaking change needs an explicit major-version and Go
   module-path migration.
2. `release-please` automatically maintains the Release PR's manifest, changelog,
   and marked README installation example. Its `skip-github-release: true`
   setting prevents it from creating a tag before release acceptance.
3. Review the Release PR's notes, compatibility and scope. CI detects an actual
   manifest version increase, independently of author, branch, title or labels.
   The increase always requires full release validation, including when every
   changed file would otherwise count as release metadata.
4. Approve the run's single `release-validation` Environment gate as a maintainer.
   Before approval, only lightweight classification and its regression tests run.
   Approval unlocks the full matrix plus bounded fuzzing and an API comparison
   against the previous release. Each new candidate run needs approval; obsolete
   runs are cancelled. The final required `quality` check rejects failures,
   cancellations, missing classification, and unexpectedly skipped checks.
5. After the complete CI succeeds, the maintainer merges the Release PR. There is
   no auto-merge. The `Publish release` workflow verifies the evidence below
   before creating an annotated tag at the merged release commit, uploading
   evidence assets, and publishing the GitHub Release.
6. `Verify release` runs once after publication. It checks main ancestry,
   tag/manifest/changelog/README agreement and a fresh public Go Proxy consumer.
   It does not repeat the full matrix. pkg.go.dev indexing can lag the proxy.

The Go 1.23/1.25 matrix, root and generator tests/race, vet/static analysis,
formatting, schema checksums, upstream lock consistency, deterministic generation,
examples, fresh local consumer, native Windows process lifecycle, and all official
SDK interop cases are required for a release. The release-only acceptance job
also replays fuzz seeds, fuzzes each target for 15 seconds with bounded parallelism,
and compares the module API using a pinned `apidiff` tool on Go 1.26.8. Unexpected
stable API incompatibilities fail; schema provenance constant value updates and
isolated experimental API changes are reported for review.

## Binding acceptance to publication

CI checks out one immutable candidate commit in every job. Successful release
validation produces `release-validation.json` containing the PR number, head/base
SHAs, tested commit/tree, version, run ID and attempt. The interop report records
the same clean checkout. Acceptance artifacts are named by run attempt so a
re-run cannot silently reuse an older attempt's evidence.

Before any tag becomes visible, `Publish release` requires:

- A merged, same-repository Release PR targeting protected `main`, with an actual
  version increase and consistent committed manifest/changelog/README.
- A successful run of this repository's `ci.yml` with every release job successful,
  including approval, acceptance, both Go versions, race/static, Windows and interop.
- GitHub's environment approval history showing a human repository maintainer
  approved `release-validation`; skipped approval is not accepted.
- Evidence matching the PR, version, run, attempt, and tested Git commit. Normal
  PR validation must have the claimed main/head merge parents. Recovery validation
  must test the exact merged release commit.
- The immutable PR head commit must be associated with that release PR through
  GitHub's commit-to-PR API. A workflow run's `pull_requests` list can become empty
  after merge, so it is not the authoritative source for this association.
- Exact equality between the tested tree and the release commit's tree. Squash
  may change the commit SHA but must not change its contents.
- Complete, clean cross-SDK evidence from that tested commit and an acceptable
  API diff. Both reports and the validation record become permanent release assets.

If concurrent changes reach main before the release merge and change the tree,
publication stops before creating a tag. The selected release commit is immutable;
later main commits are never substituted for it. Labels assist Release Please's
bookkeeping but do not grant publication authority.

## Repository and approval controls

Require the GitHub Actions `quality` check (integration ID 15368), PRs, resolved
review conversations, and no force pushes, deletion, or bypass actors on `main`.
Disable the global up-to-date requirement (`strict=false`) to avoid rebasing and
retesting every ordinary PR. Keep `refs/tags/v*` protected against deletion and
non-fast-forward updates; new tag creation remains available to the publisher.

Create the `release-validation` Environment before merging this configuration:

- Required reviewer: the maintainer user or team; initial maintainer is
  `OnslaughtSnail`. Do not give this role to the release bot.
- Disable administrator bypass. Allow `refs/pull/*/merge` and `main` through the
  Environment's selected branch policies, covering PR acceptance and recovery.
- The initiating maintainer may approve their own run, so a single maintainer can
  dispatch recovery. This is still an explicit recorded approval; no workflow
  approves itself. Add another reviewer and prevent self-review if adopting a
  two-person release policy later.
- No secrets are stored in this Environment. Validation jobs use read-only
  repository permissions and checkouts without persisted credentials.

PR code never receives the release token. Release automation runs trusted main
workflows. Publication validates GitHub API identities and parses downloaded
artifacts only as data; it never executes downloaded artifact content. The token
is made available only to the final publication step after validation succeeds.

## Release bot token

Use `RELEASE_PLEASE_TOKEN`, either a repository secret or a selected-repository
organization secret. Scope the dedicated bot fine-grained PAT to this repository
with Contents, Pull requests, and Issues read/write. Its owner needs repository
write access and any required organization approval. For a shared organization
secret, both secret visibility and the token's repository scope must include the
SDK. No ruleset bypass or administration access is required by the bot.

The ordinary `GITHUB_TOKEN` remains read-only and Actions PR approval can stay
disabled. The dedicated token lets bot PRs and published releases trigger the
follow-on CI workflows; see [official authentication guidance](https://github.com/googleapis/release-please-action#github-credentials).
The Release Please action is pinned to its verified v5.0.0 commit. Its updates
are serialized and `always-update` stays at the default, avoiding unchanged PR
refreshes. Missing-token and permission errors must be repaired without weakening
the release gates or inserting a personal CLI token into Actions secrets.

## Recovery

If the merged release tree differs from the tested candidate, or validation
artifacts have expired:

1. Dispatch `CI` on `main`, setting `release_pr` to the merged Release PR number.
   It selects that exact merge commit, not moving main.
2. Approve `release-validation` and wait for the complete matrix and `quality`.
3. Dispatch `Publish release` on `main` with `release_pr` and `validation_run`.
   The same evidence checks run before tagging.

For publication errors after validation, retry `Publish release` with the valid
run. An existing tag is accepted only if it resolves to the same selected commit;
it is never changed. A partially created draft release can be completed. An
already published version is left intact. Release Please lifecycle labels are
updated after publication.

If only Go Proxy propagation is delayed, rerun `Verify release` for that version.
Manual verification uses a new module/cache, checksum verification, `GOWORK=off`,
and the public proxy, with no local replacement or direct-VCS fallback:

```bash
bash scripts/consumer-smoke.sh v1.4.0
```

With no argument, the script tests the local checkout for PR CI. A local replacement
build alone does not prove publication. Fix published defects with a new version.

## Bootstrap

The initial manifest records published `1.3.0`, and `bootstrap-sha` is its release
commit `36a6825c7e4611d87bc26712f1efaf2dcb24eb34`. Release Please creates the first
`1.4.0` Release PR from the protocol-alignment feature. Do not pre-bump metadata
in a feature PR. [The v1.4.0 migration notes](docs/upgrading-to-v1.4.0.md) describe
the optional stable tool name and required experimental v2 message ID changes.
