// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"strings"

	"github.com/spf13/pflag"

	"github.com/eshu-hq/eshu/go/internal/cli/docs"
	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

// remoteFlagReader is the slice of a cobra command these helpers need: the
// --service-url/--api-key/--profile flags addRemoteFlags registered.
type remoteFlagReader interface {
	Flags() *pflag.FlagSet
}

// docsVerifyContainerImageResolver picks the container image truth source for
// this run. The choice is process state -- an "api" run needs a client built
// from flags -- so it is resolved here and handed to the docs package.
func docsVerifyContainerImageResolver(cmd remoteFlagReader, opts docs.VerifyOptions) doctruth.ContainerImageResolver {
	if opts.ImageTruth == "api" {
		return docs.APIContainerImageResolver(apiClientFromRemoteFlags(cmd))
	}
	return docs.LocalContainerImageResolver(opts.Path)
}

// effectiveDocsVerifyImageTruth resolves the "auto" image truth mode: a run
// that has been pointed at a remote service uses the API, everything else scans
// the local workspace manifests. An explicit mode is returned unchanged.
func effectiveDocsVerifyImageTruth(cmd remoteFlagReader, mode string) string {
	mode = docs.NormalizeImageTruthMode(mode)
	if mode == "auto" {
		if docsVerifyRemoteImageTruthConfigured(cmd) {
			return "api"
		}
		return "local"
	}
	return mode
}

// docsVerifyRemoteImageTruthConfigured reports whether this invocation was
// pointed at a remote Eshu service, by an explicitly set remote flag or by the
// service URL / API key environment variables.
func docsVerifyRemoteImageTruthConfigured(cmd remoteFlagReader) bool {
	for _, name := range []string{"service-url", "api-key", "profile"} {
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed && strings.TrimSpace(flag.Value.String()) != "" {
			return true
		}
	}
	return strings.TrimSpace(os.Getenv("ESHU_SERVICE_URL")) != "" ||
		strings.TrimSpace(os.Getenv("ESHU_API_KEY")) != ""
}

// apiClientFromRemoteFlags builds the API client from the remote flag set.
func apiClientFromRemoteFlags(cmd remoteFlagReader) *APIClient {
	return NewAPIClient(
		docsVerifyRemoteFlagValue(cmd, "service-url"),
		docsVerifyRemoteFlagValue(cmd, "api-key"),
		docsVerifyRemoteFlagValue(cmd, "profile"),
	)
}

// docsVerifyRemoteFlagValue reads one remote flag, yielding empty when the
// command does not define it.
func docsVerifyRemoteFlagValue(cmd remoteFlagReader, name string) string {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return flag.Value.String()
	}
	return ""
}
