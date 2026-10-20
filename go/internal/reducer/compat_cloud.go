// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file was the reducer root's compatibility surface for the cloud and
// platform-inventory families (awscloud, containerimage, kubernetescorrelation,
// obscoverage, cloudjoin, platformfam, shell-exec, partitioning)
// (issues #6061, #6626). It merged the per-family *_compat.go files with no
// behavior change. Every alias and forwarder was deleted once its last caller
// moved to name the owning subpackage directly; the cloud bucket is fully
// retired (see compat_alias_retirement_cloud_test.go, which pins the retired
// names absent from this package). A family move adds a stanza to the matching
// bucket file and NEVER creates a new *_compat.go
// (docs/internal/design/reducer-target-tree.md).
//
// Retired stanzas (all entries deleted, callers repointed):
//   - aws_cloud_family_compat.go (#6626 cloud bucket)
//   - container_image_identity_compat.go (#6626 cloud bucket)
//   - kubernetes_correlation_compat.go (retired earlier; #6626 batch 6 bucket 4)
//   - observability_coverage_compat.go (#6626 cloud bucket)
//   - cloud_resource_join_index_compat.go (#6626 cloud bucket)
//   - platform_compat.go (#6626 cloud bucket; relocated from compat_projection.go
//     by the code/ move, #6609)
//   - shell-exec family move (#6626 cloud bucket; relocated from
//     compat_projection.go to make room for the H5 root-remnant fold there)
//   - partitioning (H5 root-remnant fold, #6626 cloud bucket; relocated here
//     from compat_projection.go for line-budget headroom)

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
)

// containerimage re-declares the root GraphQueryRunner locally rather than
// importing it, because a family package must not import the reducer root and
// Go interfaces are satisfied structurally. That arrangement is only safe while
// the two method sets stay identical, and nothing about it is checked at the
// declaration sites -- a change to either interface would go unnoticed until
// some distant wiring site failed to compile, with an error pointing at the
// wiring rather than at the divergence.
//
// These two assignments pin it in both directions, so the method sets must
// match exactly. Either interface gaining, losing, or re-signing a method fails
// the build here, naming the real problem.
//
// containerimage.activeRepositoryFactLoader is deliberately NOT pinned: it is
// unexported, so no assertion can reach it from this package. Anyone widening
// that interface has to re-check its root counterpart by hand.
var (
	_ containerimage.GraphQueryRunner = GraphQueryRunner(nil)
	_ GraphQueryRunner                = containerimage.GraphQueryRunner(nil)
)
