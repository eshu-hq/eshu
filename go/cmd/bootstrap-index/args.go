// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
)

// bootstrapArgs is the eshu-bootstrap-index argv contract the CLI relies on.
// eshu scan and eshu index both pass --path, and both pass --force when the
// operator asks for a re-index from scratch (#7982).
type bootstrapArgs struct {
	// Path is the --path value: the workspace root the bootstrap child indexes.
	Path string
	// Force re-indexes a source that looks unchanged.
	Force bool
	// ShowHelp renders usage for --help or -h.
	ShowHelp bool
}

// parseBootstrapArgs parses argv without a flag package so the binary keeps
// its single-purpose shape: only --path, --force/-f, --help/-h, and the
// --version/-v probe handled before this. Unknown flags and positional args
// fail so a misspelled force flag cannot silently run a normal skip.
func parseBootstrapArgs(args []string) (bootstrapArgs, error) {
	var parsed bootstrapArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--force" || arg == "-f":
			parsed.Force = true
		case strings.HasPrefix(arg, "--force="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--force="))
			if err != nil {
				return bootstrapArgs{}, fmt.Errorf("invalid --force value %q: want a boolean", arg)
			}
			parsed.Force = value
		case arg == "--path":
			if i+1 >= len(args) {
				return bootstrapArgs{}, fmt.Errorf("missing value for --path")
			}
			i++
			if strings.TrimSpace(args[i]) == "" {
				return bootstrapArgs{}, fmt.Errorf("missing value for --path")
			}
			parsed.Path = args[i]
		case strings.HasPrefix(arg, "--path="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--path="))
			if value == "" {
				return bootstrapArgs{}, fmt.Errorf("missing value for --path")
			}
			parsed.Path = value
		case arg == "--help" || arg == "-h":
			parsed.ShowHelp = true
		case arg == "--version" || arg == "-v":
			return bootstrapArgs{}, fmt.Errorf("use --version alone to print the version")
		case strings.HasPrefix(arg, "-") && arg != "-":
			return bootstrapArgs{}, fmt.Errorf("unknown flag %q", arg)
		default:
			return bootstrapArgs{}, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return parsed, nil
}

// bootstrapUsage describes the argv contract the CLI depends on.
func bootstrapUsage() string {
	return "Usage: eshu-bootstrap-index [--path <root>] [--force]\n\n" +
		"  --path <root>  Workspace root to index.\n" +
		"  --force, -f    Re-index from scratch even when the source looks unchanged.\n"
}

// applyBootstrapArgOverrides applies the eshu-bootstrap-index argv contract to
// the environment-derived sync config (#7982). A --path value names the
// filesystem root the bootstrap child indexes, implying filesystem source mode
// when the environment left another mode. A --force value marks every selected
// repository for a full re-parse through the repository_reindex_requested
// reason path instead of the manifest skip.
func applyBootstrapArgOverrides(config *git.RepoSyncConfig, cliArgs bootstrapArgs) {
	if config == nil {
		return
	}
	if path := strings.TrimSpace(cliArgs.Path); path != "" {
		config.FilesystemRoot = path
		if strings.TrimSpace(config.SourceMode) != "filesystem" {
			config.SourceMode = "filesystem"
		}
		git.NormalizeFilesystemConfigForBootstrap(config)
	}
	if cliArgs.Force {
		config.ForceReindex = true
	}
}
