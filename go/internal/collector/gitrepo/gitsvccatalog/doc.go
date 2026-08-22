// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitsvccatalog emits service catalog facts from manifests found in a
// repository, such as Backstage catalog-info.yaml.
//
// It is the git-collector hook over the sibling collector/servicecatalog
// parser: that package understands the manifest formats, this one decides which
// repository files are manifests and streams the resulting facts.
package gitsvccatalog
