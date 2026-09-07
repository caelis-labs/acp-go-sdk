#!/usr/bin/env bash
set -euo pipefail
go build -o "$RUNNER_TEMP/upstreamdrift" ./cmd/upstreamdrift
set +e
"$RUNNER_TEMP/upstreamdrift" -remote -json > "$RUNNER_TEMP/upstream-drift.json"
status=$?
set -e
cat "$RUNNER_TEMP/upstream-drift.json"
if [[ "$status" -eq 0 ]]; then
  exit 0
fi
if [[ "$status" -ne 2 ]]; then
  exit "$status"
fi

gh label create upstream-drift \
  --description "Official ACP protocol or SDK pin is behind upstream" \
  --color FBCA04 || true

title=$(jq -r .issueTitle "$RUNNER_TEMP/upstream-drift.json")
jq -r .issueBody "$RUNNER_TEMP/upstream-drift.json" > "$RUNNER_TEMP/upstream-drift.md"
existing=$(gh issue list --label upstream-drift --state open --json number --jq '.[0].number // empty')
if [[ -n "$existing" ]]; then
  gh issue edit "$existing" --title "$title" --body-file "$RUNNER_TEMP/upstream-drift.md"
  echo "updated issue #$existing"
else
  url=$(gh issue create --title "$title" --body-file "$RUNNER_TEMP/upstream-drift.md" --label upstream-drift)
  echo "opened $url"
fi
exit 1
