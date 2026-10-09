// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "errors"

// ContainerImageIdentityOutcome names the reducer decision for one image
// reference seen in Git or runtime evidence.
type ContainerImageIdentityOutcome string

const (
	// ContainerImageIdentityExactDigest means the source reference already
	// named a digest also observed in registry facts.
	ContainerImageIdentityExactDigest ContainerImageIdentityOutcome = "exact_digest"
	// ContainerImageIdentityTagResolved means one registry tag observation
	// resolved the source tag to exactly one digest.
	ContainerImageIdentityTagResolved ContainerImageIdentityOutcome = "tag_resolved"
	// ContainerImageIdentityAmbiguousTag means tag observations for the same
	// image reference point at multiple digests.
	ContainerImageIdentityAmbiguousTag ContainerImageIdentityOutcome = "ambiguous_tag"
	// ContainerImageIdentityUnresolved means no registry digest observation
	// matched the source image reference.
	ContainerImageIdentityUnresolved ContainerImageIdentityOutcome = "unresolved"
	// ContainerImageIdentityStaleTag means runtime evidence resolved a tag to
	// a digest that registry facts report as the previous digest.
	ContainerImageIdentityStaleTag ContainerImageIdentityOutcome = "stale_tag"
)

// ContainerImageIdentityFactKind names the durable fact kind the
// container-image-identity writer publishes under. It is exported so
// families below the reducer root (e.g. sbomattest) can name it without
// importing the reducer root package, which would violate the strictly
// downward package-import direction (root -> family -> shared-core ->
// contract).
const ContainerImageIdentityFactKind = "reducer_container_image_identity"

// ErrContainerImageIdentityGenerationNotActive reports that the activation
// epoch read found no row for the intent's (scope, generation): the
// generation is not the scope's active one, or its scope-state row is
// missing. The epoch reader cannot tell a pending generation from a
// superseded or missing one, so callers match this sentinel with errors.Is
// and disambiguate through a GenerationFreshnessCheck: a pending generation
// defers (GenerationNotYetActiveError), a superseded one acks as superseded,
// and a generation the check still calls current re-reads once before
// surfacing loudly (issue #6502).
var ErrContainerImageIdentityGenerationNotActive = errors.New(
	"container image identity generation is not active",
)
