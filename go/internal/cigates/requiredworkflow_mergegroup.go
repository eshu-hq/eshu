// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import "fmt"

// validateBlockingWorkflowMergeGroup reports a blocking gate workflow that
// does not trigger on merge_group.
//
// main is merged through a GitHub merge queue. The trusted publisher
// aggregates a queue entry the same way it aggregates a pull request: it
// selects every blocking gate the entry's changed paths match and waits for
// that gate's merge_group check row. A workflow with no merge_group trigger
// never produces that row, so the aggregate waits out its whole timeout on a
// MISSING check, publishes nothing, and the entry is dropped from the queue
// with no red check naming why. The rule is derived from the registry's own
// blocking flag, so there is no exception list: a workflow that must not run
// on the queue has to stop being blocking.
func validateBlockingWorkflowMergeGroup(check RequiredStatusCheck, workflowFile string, raw []byte) error {
	triggers, err := workflowTriggerKeys(raw)
	if err != nil {
		return fmt.Errorf("required status context %q: parse triggers of blocking workflow %s: %w", check.Context, workflowFile, err)
	}
	if _, ok := triggers["merge_group"]; !ok {
		return fmt.Errorf(
			"required status context %q: blocking workflow %s has no merge_group trigger; "+
				"a merge-queue entry that selects its gate would wait on a check that never appears "+
				"(add `merge_group: types: [checks_requested]`, or make its gates non-blocking)",
			check.Context,
			workflowFile,
		)
	}
	return nil
}
