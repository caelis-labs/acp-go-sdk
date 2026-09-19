#!/usr/bin/env bash
set -euo pipefail

export GOWORK=off
repo_root=$(pwd)
mkdir -p .artifacts/bin
baseline=$(node -p 'JSON.parse(process.env.RELEASE_CONTEXT).base')
previous=$(git show "$baseline:.release-please-manifest.json" | node -e 'let s="";for await (const c of process.stdin) s+=c;console.log(JSON.parse(s)["."])' --input-type=module)
if [[ ! "$previous" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo 'invalid previous release version' >&2
  exit 1
fi
git merge-base --is-ancestor "refs/tags/v$previous" "$baseline"
old_dir=$(mktemp -d)
trap 'rm -rf "$old_dir"' EXIT
git archive "refs/tags/v$previous" | tar -x -C "$old_dir"
GOBIN="$repo_root/.artifacts/bin" go install golang.org/x/exp/cmd/apidiff@v0.0.0-20260824195058-e88cd73687aa
apidiff="$repo_root/.artifacts/bin/apidiff"
(cd "$old_dir" && "$apidiff" -m -w "$repo_root/.artifacts/api-before.export" github.com/caelis-labs/acp-go-sdk)
"$apidiff" -m -w .artifacts/api-after.export github.com/caelis-labs/acp-go-sdk
"$apidiff" -m .artifacts/api-before.export .artifacts/api-after.export > .artifacts/api-diff.txt
cat .artifacts/api-diff.txt
node scripts/release-api-check.mjs .artifacts/api-diff.txt
