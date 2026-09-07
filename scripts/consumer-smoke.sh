#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)
smoke_dir=$(mktemp -d)
trap 'rm -rf "$smoke_dir"' EXIT

cd "$smoke_dir"
go mod init example.com/acp-consumer
go mod edit -replace github.com/caelis-labs/acp-go-sdk="$repo_root"
go get github.com/caelis-labs/acp-go-sdk
go get github.com/caelis-labs/acp-go-sdk/transport/stdio

cat > main.go <<'EOF'
package main

import (
	"context"
	"fmt"
	"io"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
 v2 "github.com/caelis-labs/acp-go-sdk/experimental/v2"
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

go build .
