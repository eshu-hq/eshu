// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/graphinstall"
)

// runInstallNornicDB is the `eshu install nornicdb` RunE. It resolves the
// cobra flags and the exit-code contract -- the parts that must stay in
// cmd/eshu because it is `package main` -- and delegates verification and
// the actual install to graphinstall.Install, wiring localGraphReadVersion
// (local_graph_process.go) as the VersionReader so graphinstall never has to
// execute a binary itself.
func runInstallNornicDB(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("eshu install nornicdb accepts flags only, got %d argument(s)", len(args))
	}
	from, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}
	expectedSHA, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return err
	}
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	full, err := cmd.Flags().GetBool("full")
	if err != nil {
		return err
	}
	if full && strings.TrimSpace(from) != "" {
		return errors.New("--full is reserved for future no-argument release installs; install full NornicDB binaries with --from")
	}

	result, err := graphinstall.Install(graphinstall.Options{
		Context:     cmd.Context(),
		From:        from,
		SHA256:      expectedSHA,
		Force:       force,
		Full:        full,
		ReadVersion: localGraphReadVersion,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}
