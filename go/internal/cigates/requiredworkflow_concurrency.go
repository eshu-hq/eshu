// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
)

type requiredWorkflowConcurrency struct {
	Group            string `yaml:"group"`
	CancelInProgress any    `yaml:"cancel-in-progress"`
}

func validateTrustedWorkflowConcurrency(
	check RequiredStatusCheck,
	concurrency requiredWorkflowConcurrency,
) []error {
	const requiredGroup = "required-gates-${{ github.event.workflow_run.head_sha || github.ref }}"
	var errs []error
	if strings.TrimSpace(concurrency.Group) != requiredGroup {
		errs = append(errs, fmt.Errorf(
			"required status context %q: workflow concurrency group must serialize publishers by workflow_run.head_sha",
			check.Context,
		))
	}
	cancelInProgress, isLiteralBool := concurrency.CancelInProgress.(bool)
	if !isLiteralBool || cancelInProgress {
		errs = append(errs, fmt.Errorf(
			"required status context %q: workflow concurrency cancel-in-progress must be literal false",
			check.Context,
		))
	}
	return errs
}
