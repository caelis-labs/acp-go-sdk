#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)
smoke_dir=$(mktemp -d)
trap 'rm -rf "$smoke_dir"' EXIT

cd "$smoke_dir"
go mod init example.com/acp-consumer
go mod edit -replace github.com/caelis-labs/acp-go-sdk="$repo_root"
go get github.com/caelis-labs/acp-go-sdk

cat > main.go <<'EOF'
package main

import (
	"context"
	"fmt"
	"io"

	acp "github.com/caelis-labs/acp-go-sdk"
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
	fmt.Println(acp.WireProtocolVersion, block.Text.Text)
}
EOF

go build .
