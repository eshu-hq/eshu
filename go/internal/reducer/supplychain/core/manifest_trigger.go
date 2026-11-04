// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

// The manifest-dependency trigger (#9874, arbiter ruling Option B). The
// projector enqueues one supply_chain_impact intent per git scope generation
// whose only trigger is a manifest or lockfile dependency row, and stamps the
// payload with this constant marker. The handler branches on it before it
// loads any evidence: it writes no findings and only reopens the scopes that
// hold advisory evidence for the packages the generation changed.
const (
	// PayloadKeyTrigger is the reducer work-item payload key the projector sets
	// on a marker intent.
	PayloadKeyTrigger = "trigger"
	// TriggerManifestDependency is the PayloadKeyTrigger value for a git
	// generation whose only trigger fact is a dependency row.
	TriggerManifestDependency = "manifest_dependency"
)
