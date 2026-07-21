// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// checkRemoteValidation runs the remote_validation artifact-existence gate
// (#5407, PR 2 of #5336): every remote_validation proof-ID cited in the
// capability matrix must resolve to a committed
// docs/internal/remote-validation/<ref>.md artifact or be listed in the
// burn-down baseline at baselinePath. It enforces two independent growth
// guards. First, the ratcheting FROZEN_MAX ceiling: a baseline whose entry
// count exceeds the committed ceiling has GROWN and fails the gate. Second, and
// crucially, the immutable frozen set beside the baseline
// (RemoteValidationFrozenFileName): every baseline entry MUST be in it, so a
// NEW unbacked claim cannot be baselined even at constant entry count — the
// atomic swap of burning down one baselined ref while adding another (which the
// ceiling alone cannot catch) is rejected because the added ref is absent from
// the frozen set. The frozen set loads fail-closed. With update=true it
// regenerates the baseline from the current tree, ratcheting the ceiling down
// to the new count without ever raising it; it never writes the frozen set.
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
	// The frozen set lives beside the baseline and is the membership authority:
	// every baseline entry MUST be in it. Loading fails closed (a missing or
	// malformed frozen file is a hard error), so the atomic-swap defense can
	// never be silently absent (#5407).
	frozenPath := filepath.Join(filepath.Dir(baselinePath), capabilitycatalog.RemoteValidationFrozenFileName)
	frozen, err := capabilitycatalog.LoadRemoteValidationFrozenSet(frozenPath)
	if err != nil {
		return err
	}

	findings := capabilitycatalog.CheckRemoteValidationArtifacts(matrix, repoRoot, baseline.Entries)
	ceilingExceeded := capabilitycatalog.RemoteValidationBaselineCeilingExceeded(baseline)
	notFrozen := capabilitycatalog.RemoteValidationBaselineNotFrozen(baseline, frozen)
	if len(findings) == 0 && !ceilingExceeded && len(notFrozen) == 0 {
		_, err := fmt.Fprintf(stdout, "remote_validation artifacts verified: %d/%d baseline entr(y/ies) at or under FROZEN_MAX, all in frozen set\n", len(baseline.Entries), baseline.Ceiling)
		return err
	}
	if len(notFrozen) > 0 {
		_, _ = fmt.Fprintf(stdout,
			"%d baseline entr(y/ies) NOT in the frozen set %s (baseline ⊄ frozen): a new unbacked claim was baselined. A ref may only be baselined if it is in the frozen set; add its committed artifact instead of baselining a new claim.\n",
			len(notFrozen), frozenPath)
		for _, ref := range notFrozen {
			_, _ = fmt.Fprintf(stdout, "  %s (not in frozen set)\n", ref)
		}
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
	return fmt.Errorf("remote_validation artifact-existence gate failed: %d dangling finding(s), ceiling_exceeded=%v, not_frozen=%d", len(findings), ceilingExceeded, len(notFrozen))
}
