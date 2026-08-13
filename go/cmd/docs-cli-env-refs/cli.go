// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

const maxCLICommands = 512

var helpLongFlagPattern = regexp.MustCompile(`--[A-Za-z0-9][A-Za-z0-9-]*`)

func collectCLIFlags(ctx context.Context, binary string) (map[string]map[string]struct{}, error) {
	queue := [][]string{{}}
	seen := map[string]struct{}{}
	flags := map[string]map[string]struct{}{}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		key := strings.Join(path, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if len(seen) > maxCLICommands {
			return nil, fmt.Errorf("CLI command tree exceeds %d commands", maxCLICommands)
		}
		args := append(append([]string{}, path...), "--help")
		output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput() // #nosec G204 -- binary is an explicit verifier input and args come only from the CLI's own help tree
		if err != nil {
			return nil, fmt.Errorf("inspect eshu %s help: %w: %s", strings.Join(path, " "), err, strings.TrimSpace(string(output)))
		}
		commandFlags, subcommands := parseCLIHelp(string(output))
		commandKey := strings.Join(path, "/")
		flags[commandKey] = map[string]struct{}{}
		for _, flag := range commandFlags {
			flags[commandKey][flag] = struct{}{}
		}
		for _, subcommand := range subcommands {
			child := append(append([]string{}, path...), subcommand)
			queue = append(queue, child)
		}
	}
	return flags, nil
}

func parseCLIHelp(help string) ([]string, []string) {
	section := ""
	flagSet := map[string]struct{}{}
	commandSet := map[string]struct{}{}
	for _, line := range strings.Split(help, "\n") {
		switch strings.TrimSpace(line) {
		case "Available Commands:":
			section = "commands"
			continue
		case "Flags:", "Global Flags:":
			section = "flags"
			continue
		case "Usage:", "Aliases:", "Examples:", "Additional help topics:":
			section = ""
			continue
		case "":
			section = ""
			continue
		}
		switch section {
		case "commands":
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] != "help" {
				commandSet[fields[0]] = struct{}{}
			}
		case "flags":
			if flag := helpLongFlagPattern.FindString(line); flag != "" {
				flagSet[flag] = struct{}{}
			}
		}
	}
	flags := sortedKeys(flagSet)
	commands := sortedKeys(commandSet)
	return flags, commands
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
