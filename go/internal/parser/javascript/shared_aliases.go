// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

var (
	appendBucket = shared.AppendBucket
	basePayload  = shared.BasePayload
	cloneNode    = shared.CloneNode
	nodeEndLine  = shared.NodeEndLine
	nodeLine     = shared.NodeLine
	nodeText     = shared.NodeText
	// normalizeLineEndings is needed for sibling/config files this package
	// reads with its own os.ReadFile, which therefore skip shared.ReadSource
	// and its bare-CR normalization (issue #6306).
	normalizeLineEndings = shared.NormalizeLineEndings
	readSource           = shared.ReadSource
	sortNamedBucket      = shared.SortNamedBucket
	walkNamed            = shared.WalkNamed
)
