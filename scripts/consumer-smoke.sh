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
    "fmt"

    acp "github.com/caelis-labs/acp-go-sdk"
)

func main() {
    block := acp.TextBlock("consumer")
    fmt.Println(acp.WireProtocolVersion, block.Text.Text)
}
EOF

go build .
