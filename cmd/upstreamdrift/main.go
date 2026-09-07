// Command upstreamdrift verifies committed official ACP pins and optionally
// reports newer GitHub tags and releases.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const exitDrift = 2

func main() {
	repoRoot := flag.String("repo", ".", "repository root containing schema/, interop/, and upstream/")
	lockPath := flag.String("lock", "upstream/lock.json", "upstream lock path relative to the repository root")
	remote := flag.Bool("remote", false, "query GitHub for newer official tags and releases")
	jsonOut := flag.Bool("json", false, "write a machine-readable report to stdout")
	timeout := flag.Duration("timeout", 30*time.Second, "timeout for remote GitHub queries")
	apiURL := flag.String("github-api", "", "GitHub API base URL (defaults to https://api.github.com or ACP_UPSTREAM_GITHUB_API)")
	flag.Parse()

	if err := run(*repoRoot, *lockPath, *remote, *jsonOut, *timeout, *apiURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if isDrift(err) {
			os.Exit(exitDrift)
		}
		os.Exit(1)
	}
}

type driftError struct {
	report report
}

func (e driftError) Error() string {
	return e.report.IssueTitle
}

func isDrift(err error) bool {
	var drift driftError
	return errors.As(err, &drift)
}

func run(repoRoot, lockPath string, remote, jsonOut bool, timeout time.Duration, apiURL string) error {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	if !filepath.IsAbs(lockPath) {
		lockPath = filepath.Join(repoRoot, lockPath)
	}
	lock, err := loadLock(lockPath)
	if err != nil {
		return err
	}
	if err := checkLocal(repoRoot, lock); err != nil {
		return err
	}

	result := buildReport(nil)
	if remote {
		if apiURL == "" {
			apiURL = os.Getenv("ACP_UPSTREAM_GITHUB_API")
		}
		if apiURL == "" {
			apiURL = defaultGitHubAPI
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		findings, err := checkRemote(ctx, &githubClient{
			http:    &http.Client{Timeout: timeout},
			baseURL: apiURL,
			token:   os.Getenv("GITHUB_TOKEN"),
		}, lock)
		if err != nil {
			return err
		}
		result = buildReport(findings)
	}

	if jsonOut {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
			return err
		}
	} else if !remote {
		fmt.Println("upstream lock is consistent")
	} else if result.Status == "current" {
		fmt.Println("upstream pins match official releases")
	} else {
		fmt.Print(result.IssueBody)
	}

	if result.Status == "drift" {
		return driftError{report: result}
	}
	return nil
}
