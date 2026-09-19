#!/usr/bin/env bash
set -euo pipefail

version=${1:-}
if [[ $# -gt 1 || ( -n "$version" && ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ) ]]; then
  echo 'usage: consumer-smoke.sh [vMAJOR.MINOR.PATCH]' >&2
  exit 1
fi

repo_root=$(cd "$(dirname "$0")/.." && pwd)
smoke_dir=$(mktemp -d)
trap 'chmod -R u+w "$smoke_dir"; rm -rf "$smoke_dir"' EXIT

export GOWORK=off
cd "$smoke_dir"
go mod init example.com/acp-consumer
if [[ -n "$version" ]]; then
  # No local replace or direct-VCS fallback may hide a missing publication.
  export GOMODCACHE="$smoke_dir/modcache"
  export GOPROXY=https://proxy.golang.org
  export GOSUMDB=sum.golang.org
  export GOPRIVATE= GONOPROXY= GONOSUMDB=
  go get "github.com/caelis-labs/acp-go-sdk@$version"
  resolved=$(go list -m -f '{{.Version}} {{if .Replace}}replaced{{end}}' github.com/caelis-labs/acp-go-sdk)
  if [[ "$resolved" != "$version " ]]; then
    echo "unexpected public module resolution: $resolved" >&2
    exit 1
  fi
else
  go mod edit -replace github.com/caelis-labs/acp-go-sdk="$repo_root"
  go get github.com/caelis-labs/acp-go-sdk
fi

cat > main.go <<'EOF'
package main

import (
	"context"
	"fmt"
	"io"

	acp "github.com/caelis-labs/acp-go-sdk"
	v2 "github.com/caelis-labs/acp-go-sdk/experimental/v2"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

func main() {
	block := acp.TextBlock("consumer")
	reader, keepOpen := io.Pipe()
	defer keepOpen.Close()
	connection := acp.NewConnection(nil, io.Discard, reader)
	defer connection.Close()
	request, err := acp.PrepareRequest[struct{}](connection, "consumer/smoke", nil)
	if err != nil {
		panic(err)
	}
	if state := request.Abandon(); state != acp.RequestSubmissionNotStarted {
		panic(state)
	}
	_, _ = request.Wait(context.Background())
	_, _ = acp.InboundParamsFromContext(context.Background())
	_ = (*acp.AgentSideConnection).SessionUpdateRaw
	_ = (*acp.Connection).SendTransportFrame
	_ = (*v2.AgentSideConnection).CreateElicitation
	_ = (*v2.ClientSideConnection).LoginAuth
	_ = v2.RunningUpdate()
	_ = (*stdio.Process).Shutdown
	_ = (*stdio.ClientProcess).Shutdown
	fmt.Println(acp.WireProtocolVersion, block.Text.Text)
}
EOF

go mod tidy
go build .
