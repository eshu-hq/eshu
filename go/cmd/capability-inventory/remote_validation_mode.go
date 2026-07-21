// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// checkRemoteValidation runs the remote_validation artifact-existence gate
// (#5407, PR 2 of #5336): every remote_validation proof-ID cited in the
// capability matrix must resolve to a committed
// docs/internal/remote-validation/<ref>.md artifact or be listed in the
// burn-down baseline at baselinePath. With update=true it regenerates the
// baseline from the current tree instead of checking it.
func checkRemoteValidation(stdout io.Writer, specsDir, repoRoot, baselinePath string, update bool) error {
	matrix, err := capabilitycatalog.LoadMatrix(specsDir)
	if err != nil {
		return err
	}

	if update {
		rendered := capabilitycatalog.RenderRemoteValidationBaseline(matrix, repoRoot)
		if err := os.WriteFile(baselinePath, []byte(rendered), 0o600); err != nil {
			return fmt.Errorf("write remote-validation baseline %s: %w", baselinePath, err)
		}
		_, err := fmt.Fprintf(stdout, "wrote %s\n", baselinePath)
		return err
	}

	baseline, err := capabilitycatalog.LoadRemoteValidationBaseline(baselinePath)
	if err != nil {
		return err
	}
	findings := capabilitycatalog.CheckRemoteValidationArtifacts(matrix, repoRoot, baseline)
	if len(findings) == 0 {
		_, err := fmt.Fprintf(stdout, "remote_validation artifacts verified: %d baseline entr(y/ies)\n", len(baseline))
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%d dangling remote_validation ref(s) not in %s:\n", len(findings), baselinePath)
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "  %s (cited by %s)\n", finding.Ref, strings.Join(finding.Subjects, ", "))
	}
	return fmt.Errorf("remote_validation artifact-existence gate failed: %d finding(s)", len(findings))
}
