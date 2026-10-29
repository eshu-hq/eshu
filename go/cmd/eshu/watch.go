// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
	"github.com/eshu-hq/eshu/go/internal/eshulocal"
)

// runWatch resolves the workspace root for a local path and hands off to the
// retained core local-host entrypoint. It lives in this file, apart from the
// pure-remote verbs in basic.go, so that file carries no eshulocal import and
// the pure-remote subset can populate eshu-cli without a core handle.
func runWatch(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	explicitRoot, err := cmd.Flags().GetString("workspace-root")
	if err != nil {
		return err
	}
	workspaceRoot, err := eshulocal.ResolveWorkspaceRoot(path, explicitRoot)
	if err != nil {
		return err
	}

	binary, err := procexec.Executable()
	if err != nil {
		printError("eshu executable not found.")
		return fmt.Errorf("eshu executable not found")
	}

	fmt.Printf("Watching %s for changes...\n", workspaceRoot)
	return procexec.Exec(binary, []string{procexec.CleanExecutableArg0(binary), "local-host", "watch", workspaceRoot}, procexec.Environ())
}
