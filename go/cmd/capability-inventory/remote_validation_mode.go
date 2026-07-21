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
// burn-down baseline at baselinePath. It also enforces the baseline's
// ratcheting FROZEN_MAX ceiling: a baseline whose entry count exceeds the
// committed ceiling has GROWN and fails the gate, so a new unverified
// production:supported row cannot be smuggled in by appending its ref and
// regenerating. With update=true it regenerates the baseline from the current
// tree, ratcheting the ceiling down to the new count without ever raising it.
func checkRemoteValidation(stdout io.Writer, specsDir, repoRoot, baselinePath string, update bool) error {
	matrix, err := capabilitycatalog.LoadMatrix(specsDir)
	if err != nil {
		return err
	}

	if update {
		// Read the prior FROZEN_MAX so regeneration can ratchet the ceiling
		// DOWN without ever raising it. A not-found result (missing file or a
		// pre-gate baseline with no directive) means "no prior bound", so the
		// rendered ceiling becomes the current dangling count.
		priorCeiling, priorFound := capabilitycatalog.ReadRemoteValidationCeiling(baselinePath)
		rendered := capabilitycatalog.RenderRemoteValidationBaseline(matrix, repoRoot, priorCeiling, priorFound)
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
	findings := capabilitycatalog.CheckRemoteValidationArtifacts(matrix, repoRoot, baseline.Entries)
	ceilingExceeded := capabilitycatalog.RemoteValidationBaselineCeilingExceeded(baseline)
	if len(findings) == 0 && !ceilingExceeded {
		_, err := fmt.Fprintf(stdout, "remote_validation artifacts verified: %d/%d baseline entr(y/ies) at or under FROZEN_MAX\n", len(baseline.Entries), baseline.Ceiling)
		return err
	}
	if ceilingExceeded {
		_, _ = fmt.Fprintf(stdout,
			"baseline entry count %d EXCEEDS frozen ceiling %d (FROZEN_MAX) in %s: the burn-down set grew. Commit an artifact to shrink it, or raise FROZEN_MAX in an explicit, reviewed edit.\n",
			len(baseline.Entries), baseline.Ceiling, baselinePath)
	}
	if len(findings) > 0 {
		_, _ = fmt.Fprintf(stdout, "%d dangling remote_validation ref(s) not in %s:\n", len(findings), baselinePath)
		for _, finding := range findings {
			_, _ = fmt.Fprintf(stdout, "  %s (cited by %s)\n", finding.Ref, strings.Join(finding.Subjects, ", "))
		}
	}
	return fmt.Errorf("remote_validation artifact-existence gate failed: %d dangling finding(s), ceiling_exceeded=%v", len(findings), ceilingExceeded)
}
