# Releasing ACP Go SDK

ACP Go SDK releases are immutable Go module versions. Once a public tag reaches
GitHub, Go proxies and downstream caches may retain it indefinitely. Never move,
delete, or reuse a published tag.

## Normal release flow

1. Open a PR against `main`. Use a Conventional Commit PR title and squash merge:
   `feat: ...` selects a minor version, `fix: ...` selects a patch, and
   `chore: ...` / `docs: ...` do not normally start a release. A stable API breaking
   change requires an explicit major-version and Go module-path migration.
2. The required `quality` check validates the actual PR diff. Code, generated
   protocol, schema, dependency, script, workflow, and unknown-path changes run
   the full Go 1.23/1.25, race/static, Windows stdio, and official TypeScript/Rust
   interoperability checks. Interop evidence remains a CI artifact. Prose and
   release metadata run the classifier's regression tests, metadata consistency,
   and whitespace checks. Classification or any selected check failing blocks
   merging; a skipped code check cannot satisfy a code PR.
3. After merge, `release-please` maintains a Release PR on `main`. It updates
   `CHANGELOG.md`, `.release-please-manifest.json`, and the marked installation
   example in `README.md`. Review the version, notes, compatibility changes, and
   relevant code PR's interoperability evidence. Add migration detail to the
   generated changelog before merging when needed.
4. Merge the Release PR after its `quality` check passes. This is the publication
   decision. Release Please creates the version tag and GitHub Release at the
   merged release commit. No auto-merge or separate environment approval is used.
5. `Verify release` runs once on `release.published`: it checks `main` ancestry,
   tag/manifest/changelog/README agreement, and a fresh public Go proxy consumer
   importing stable, experimental v2, and stdio packages. It does not rerun the
   full test matrix. Check pkg.go.dev indexing separately; it may lag the proxy.

Ordinary pushes to `main` run only Release Please, not another full CI run. Its
updates are serialized and are not forced when the release notes are unchanged
(`always-update` is left at its default). Obsolete PR CI runs are cancelled.
Schema checksum verification, upstream lock validation, generation, formatting,
and vet run once in the full pipeline, rather than once per Go matrix entry.

This replaces the former manual exact-main-SHA tagging workflow. The release
trust boundary is now protected PR merges into `main`: a release-metadata-only
PR reuses the already reviewed code and conformance evidence. Publication checks
observe an already public version; they are not a substitute for pre-merge CI.

## Bootstrap and migration notes

The initial manifest records the already published `1.3.0`, and `bootstrap-sha`
is its release commit, `36a6825c7e4611d87bc26712f1efaf2dcb24eb34`. Do not pre-bump
the manifest or add an unreleased numbered changelog heading in a feature PR.
Release Please owns those changes. The next `feat:` merge produces `v1.4.0`.
[The v1.4.0 migration notes](docs/upgrading-to-v1.4.0.md) retain the stable tool
name and experimental v2 message-ID compatibility details.

An exceptional version override can use a reviewed `Release-As: 1.4.0` footer
in a squash commit. Do not leave a persistent `release-as` config override.
Experimental v2 changes remain isolated from the stable root package and must
be explained in release notes even though they do not change the stable ACP
wire protocol version.

## Repository controls

- Protect `main` with the GitHub Actions `quality` check (integration ID 15368),
  a PR requirement, resolved review conversations, and no force pushes/deletion
  or bypass actors. Keep the aggregate check's name stable.
- Do not require an up-to-date branch for every PR. A movement of `main` alone
  should not force another full matrix; refresh and rerun when concurrent
  changes could affect the PR. Review interacting changes before merging.
- Keep `refs/tags/v*` protected against deletion and non-fast-forward updates.
  Allow creation so the release bot can publish a new version.
- During migration, wait until the new `quality` check succeeds, then replace
  the five individual required job names with `quality` in the ruleset. Keep
  the old requirements until the aggregate is available; otherwise PRs may
  lose their gate or wait for a check that does not exist.
- Use squash merges with the PR title as the commit title. For merge/rebase
  workflows, maintain Conventional Commits in the actual commits as well.

CI uses `pull_request` with `contents: read`, no release secret, and checkouts
without persisted credentials. There is no bot/label/title-based test bypass or
`pull_request_target` execution of PR code. New or unknown files select full CI.
The classifier uses the PR base-to-merge diff, including deletions and renames;
metadata must be regular files and the version must increase in a Release PR.

## Release bot permissions

Provide `RELEASE_PLEASE_TOKEN` as a repository secret or an organization secret
whose selected repositories include `caelis-labs/acp-go-sdk`. Prefer a dedicated
bot fine-grained PAT limited to this repository with:

- Contents: read and write (release branch, tag, GitHub Release).
- Pull requests: read and write (maintain Release PR).
- Issues: read and write (Release Please lifecycle labels).

The token owner needs repository write access; satisfy any organization PAT
approval policy. For an organization secret shared with Caelis, include both
repositories in the token's repository scope as well as the secret's visibility.
Secret visibility alone does not prove that the PAT can write to this repo.
No administration permission, ruleset bypass, workflow-write permission,
self-approval, or auto-merge permission is required. The ordinary workflow
`GITHUB_TOKEN` stays read-only, and Actions PR approval can remain disabled.

Use this dedicated token rather than `GITHUB_TOKEN`: GitHub suppresses most
follow-on workflows created by `GITHUB_TOKEN`, which would prevent the bot's
Release PR from receiving CI and the published release from being verified.
See the [official action authentication guidance](https://github.com/googleapis/release-please-action#github-credentials).
The action is pinned to the verified `v5.0.0` commit. Only pushes or manual runs
on `main` can invoke it; no PR job can access the token.

## Recovery and publication verification

If a token is missing, the workflow fails with a setup message. A 403 after that
means the token's scope, owner access, expiry, or organization approval needs
attention. Fix the configuration, then manually run `release-please` on `main`.
Do not weaken branch protection, inject a personal CLI token, or switch to
`GITHUB_TOKEN` as a workaround. Retrying Release Please reconciles existing
release state; inspect any partially created tag/release first.

If the public proxy is still propagating, rerun only `Verify release` or dispatch
it on `main` with the published version. The smoke test uses a new module and
module cache, `GOWORK=off`, `GOPROXY=https://proxy.golang.org`, checksum verification,
and no `replace` or direct-VCS fallback:

```bash
bash scripts/consumer-smoke.sh v1.4.0
```

With no argument the same script tests the local checkout for PR CI. A local
`replace` build does not establish that a version has been published.

For an exceptional manual recovery, prepare manifest/changelog/README in a PR,
pass `quality`, merge it, and record the exact protected-main commit and code
PR's conformance evidence. Check that the version is unused before creating an
annotated tag and GitHub Release at that commit. Never create a replacement for
an existing tag. Fix released defects with a new semantic version.
