# Releasing ACP Go SDK

ACP Go SDK releases are immutable Go module versions. A public tag can be
cached by `proxy.golang.org`, `sum.golang.org`, pkg.go.dev, and downstream
module caches, so a published tag must never be moved or reused.

## Release invariant

A release tag must point to an exact commit that:

- is contained in `origin/main`;
- already contains the final README, changelog, schema lock, generated code,
  and other release metadata;
- has a successful push-triggered `CI` workflow for that exact commit SHA; and
- has not previously been published under the requested version.

The tag is created only after those conditions are true. Tag-triggered CI is
not a substitute because a tag may already be visible to the Go module mirror
before that CI completes.

## Repository controls

GitHub repository settings reinforce the release invariant:

- `main` requires the GitHub Actions checks `test (1.23.x)`, `test (1.25.x)`,
  `race-and-static`, `windows-stdio`, and `official-sdk-interop`. Required
  branches must be current, history must remain linear, conversations must be
  resolved, and force pushes and deletion are disabled.
- The active `Protect release tags` ruleset matches `refs/tags/v*` and rejects
  deletion and non-fast-forward updates. It permits creation so that the
  exact-SHA release workflow can publish a new version.

If a CI job is renamed, update the required-check configuration in the same
maintenance window so pull requests do not become permanently blocked.

## Prepare and verify the release commit

1. Update `CHANGELOG.md` and any version-specific README examples. Complete all
   schema, generated-code, compatibility, and attribution changes before the
   release commit is made.
2. Run the repository validation required by `AGENTS.md`.
3. Commit and push the release commit to `main` through the normal reviewed
   path.
4. Record the exact commit and wait for its `CI` workflow to finish:

   ```bash
   commit=$(git rev-parse origin/main)
   gh run list --workflow CI --commit "$commit"
   gh run watch --exit-status <run-id>
   ```

5. Confirm that local, remote, and CI identities are the same:

   ```bash
   test "$(git rev-parse HEAD)" = "$commit"
   test "$(git rev-parse origin/main)" = "$commit"
   gh run view <run-id> --json headSha,conclusion
   ```

Do not amend the commit or make a follow-up documentation commit after this
check. Any change produces a new SHA that must be pushed and validated again.

## Create the tag

Use the GitHub Actions `Create release tag` workflow. Supply:

- `version`: a semantic Go module version beginning with `v`, such as
  `v1.1.0` or `v1.2.0-rc.1`;
- `commit`: the complete 40-character commit SHA verified above.

The workflow independently verifies version syntax, `main` ancestry, an exact
successful push-triggered `CI` run, and tag non-existence. It then creates and
pushes an annotated tag. It does not create tags while validating pull requests
or ordinary pushes.

If manual recovery is ever necessary, perform the same checks and tag the exact
SHA explicitly rather than the moving `main` name:

```bash
git tag -a v1.1.0 <40-character-commit-sha> -m v1.1.0
git push origin refs/tags/v1.1.0
```

## Verify publication

After the tag exists, ask the public Go proxy for that exact version:

```bash
GOPROXY=https://proxy.golang.org go list -m -json \
  github.com/caelis-labs/acp-go-sdk@v1.1.0
```

Then verify:

- `https://proxy.golang.org/github.com/caelis-labs/acp-go-sdk/@v/v1.1.0.info`;
- `https://pkg.go.dev/github.com/caelis-labs/acp-go-sdk@v1.1.0`;
- a fresh consumer module can resolve, compile, and test the version.

The default pkg.go.dev page may take several minutes to update. Stable releases
take precedence over prereleases when pkg.go.dev chooses the default version.

## Failure policy

- If CI fails, fix the problem in a new commit, push it, and wait for the new
  exact SHA to pass. Do not create the tag.
- If tag creation fails before the remote tag exists, correct the release
  workflow or permissions and retry with the same SHA.
- If a tag has reached the remote, never move, delete, or reuse it. Publish a
  new semantic version when correction is required.
