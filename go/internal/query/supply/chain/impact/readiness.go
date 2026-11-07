// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// ReadinessQuery is the bounded readiness lookup the handler
// runs alongside the findings page. Status is intentionally not used by
// the source-fact counts because source facts have no impact-status field;
// it is preserved here so the call site can build the query from the same
// scope value used to echo packet.TargetScope back to the caller. AdvisoryID narrows
// source-advisory rows only when another fact anchor is present.
type ReadinessQuery struct {
	CVEID         string
	AdvisoryID    string
	PackageID     string
	RepositoryID  string
	SubjectDigest string
	ImageRef      string
	Ecosystem     string
	WorkloadID    string
	ServiceID     string
	Environment   string
	Severity      string
	Status        string
}

// hasFactAnchor reports whether the query carries an anchor that source facts
// can be filtered by. impact_status is a reducer-finding attribute that does
// not appear on source facts, so an impact_status-only query has no anchor.
func (q ReadinessQuery) hasFactAnchor() bool {
	return strings.TrimSpace(q.CVEID) != "" ||
		strings.TrimSpace(q.PackageID) != "" ||
		strings.TrimSpace(q.RepositoryID) != "" ||
		strings.TrimSpace(q.SubjectDigest) != "" ||
		strings.TrimSpace(q.ImageRef) != ""
}

// ReadinessStore reads bounded source-fact counts so the
// handler can build a readiness envelope without traversing the graph.
type ReadinessStore interface {
	ReadSupplyChainImpactReadiness(
		context.Context,
		ReadinessQuery,
	) (packet.ReadinessSnapshot, error)
}
