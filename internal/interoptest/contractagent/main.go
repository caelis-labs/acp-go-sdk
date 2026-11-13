package main

import (
	"errors"
	"flag"
	"fmt"
	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/internal/interoptest/contract"
	"os"
)

func main() {
	v := flag.Int("version", 2, "wire profile")
	profile := flag.String("profile", "full", "system prompt capabilities")
	updates := flag.String("updates", "interop/contract/updates.json", "wire vectors")
	flag.Parse()
	if err := contract.Serve(*v, *profile, *updates); err != nil && !errors.Is(err, acp.ErrPeerClosed) && !errors.Is(err, acp.ErrConnectionClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
