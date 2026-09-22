// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"fmt"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// CloudResourceRetracter deletes globally-dead CloudResource node uids under
// the #5007 owner-ledger gate for the shared AWS/GCP/Azure writer. Liveness
// is the admission-uid probe baked in: a uid dies only when no scope's
// current generation admits it. It satisfies the reducer's
// CloudResourceNodeRetracter consumer interface.
type CloudResourceRetracter struct {
	gate        *Gate
	deleteNodes nodeDeleteFunc
	// requireDrained is the pre-lock admission-drain fence
	// (postgres.RequireCloudAdmissionDrained in production; tests inject a
	// stand-in). It runs in its own short transaction before any per-uid
	// lock so a refusal costs one index probe, not a 500-lock acquisition.
	requireDrained func(context.Context, db.ExecQueryer) error
}

// NewCloudResourceRetracter gates deleteNodes (the raw cypher writer's
// RetractCloudResourceNodes method) on the owner ledger via gate with the
// admission-uid liveness probe.
func NewCloudResourceRetracter(gate *Gate, deleteNodes nodeDeleteFunc) *CloudResourceRetracter {
	return &CloudResourceRetracter{gate: gate, deleteNodes: deleteNodes, requireDrained: postgres.RequireCloudAdmissionDrained}
}

// RetractDeadCloudResourceNodes first refuses, before any lock, while some
// scope's active-generation cloud_inventory_admission item is nonterminal
// (the error unwraps to reducercontract.ErrCloudAdmissionUndrained, which
// the handler classifies as a non-counting readiness miss). It then resolves
// liveness inside each per-uid lock-holding transaction — where the fence is
// re-read in the same statement as the admission rows — releases the dead
// uids from the ledger, and deletes them from the graph. It returns the
// number of deleted nodes.
func (w *CloudResourceRetracter) RetractDeadCloudResourceNodes(
	ctx context.Context,
	uids []string,
	evidenceSource string,
) (int, error) {
	if len(uids) == 0 || w.gate == nil || w.gate.database == nil {
		// Same fail-closed skip as the gate itself: nothing to retract, or no
		// ledger to prove death against.
		return w.gate.RetractDeadUIDs(ctx, familyCloudResource, uids, evidenceSource,
			postgres.LiveAdmissionCloudUIDs, w.deleteNodes)
	}
	if err := w.checkDrained(ctx); err != nil {
		return 0, err
	}
	return w.gate.RetractDeadUIDs(
		ctx, familyCloudResource, uids, evidenceSource,
		postgres.LiveAdmissionCloudUIDs, w.deleteNodes,
	)
}

// checkDrained runs the pre-lock fence in a short transaction that is always
// rolled back: it reads nothing the retract chunk will not re-read under
// lock, so it only exists to make a refusal cheap.
func (w *CloudResourceRetracter) checkDrained(ctx context.Context) error {
	tx, err := w.gate.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("graphowner: begin admission-drain check: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := w.requireDrained(ctx, tx); err != nil {
		return fmt.Errorf("graphowner: admission-drain check for %s: %w", familyCloudResource, err)
	}
	return nil
}

// EC2InstanceRetracter deletes globally-dead EC2 instance CloudResource node
// uids under the owner-ledger gate. Liveness is the EC2-tuple probe baked
// in: a uid dies only when no scope's current generation carries its posture
// identity tuple. It satisfies the ec2instance EC2InstanceNodeRetracter
// consumer interface.
type EC2InstanceRetracter struct {
	gate        *Gate
	deleteNodes nodeDeleteFunc
}

// NewEC2InstanceRetracter gates deleteNodes (the raw cypher writer's
// RetractEC2InstanceNodes method) on the owner ledger via gate with the
// EC2-posture-tuple liveness probe.
func NewEC2InstanceRetracter(gate *Gate, deleteNodes nodeDeleteFunc) *EC2InstanceRetracter {
	return &EC2InstanceRetracter{gate: gate, deleteNodes: deleteNodes}
}

// RetractDeadEC2InstanceNodes resolves tuple liveness inside the per-uid
// lock-holding transaction, releases the dead uids from the ledger, and
// deletes them from the graph. The Gate chunks by uid; the liveness closure
// maps each chunk back to its posture tuples, so the probe always sees the
// exact chunk under lock. It returns the number of deleted nodes.
func (w *EC2InstanceRetracter) RetractDeadEC2InstanceNodes(
	ctx context.Context,
	candidates []reducercontract.EC2PostureCandidate,
	evidenceSource string,
) (int, error) {
	byUID := make(map[string]reducercontract.EC2PostureCandidate, len(candidates))
	uids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c.UID == "" {
			continue
		}
		if _, seen := byUID[c.UID]; !seen {
			byUID[c.UID] = c
			uids = append(uids, c.UID)
		}
	}
	checkAlive := func(ctx context.Context, tx db.ExecQueryer, chunk []string) (map[string]struct{}, error) {
		chunkCandidates := make([]reducercontract.EC2PostureCandidate, 0, len(chunk))
		for _, uid := range chunk {
			if c, ok := byUID[uid]; ok {
				chunkCandidates = append(chunkCandidates, c)
			}
		}
		return postgres.LiveEC2PostureUIDs(ctx, tx, chunkCandidates)
	}
	return w.gate.RetractDeadUIDs(
		ctx, familyEC2Instance, uids, evidenceSource,
		checkAlive, w.deleteNodes,
	)
}
