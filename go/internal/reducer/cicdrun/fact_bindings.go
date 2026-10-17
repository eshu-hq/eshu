// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// CICDRunHandleBinding declares this package's verified reducer consumption of
// ci_cd_run kinds (issue #6629). Build-time metadata only.
var CICDRunHandleBinding = contract.Bind(contract.Binding{
	ID: "cicdrun/cicdrun_handle", FamilyID: "ci_cd_run",
	ReducerDir: "go/internal/reducer/cicdrun", Symbol: "Handle",
	Kinds: []string{"ci.artifact", "ci.deployment_event", "ci.environment_observation", "ci.run", "ci.step", "ci.trigger_edge", "ci.workflow_image_evidence"},
})
