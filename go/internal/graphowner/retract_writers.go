// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"

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
}

// NewCloudResourceRetracter gates deleteNodes (the raw cypher writer's
// RetractCloudResourceNodes method) on the owner ledger via gate with the
// admission-uid liveness probe.
func NewCloudResourceRetracter(gate *Gate, deleteNodes nodeDeleteFunc) *CloudResourceRetracter {
	return &CloudResourceRetracter{gate: gate, deleteNodes: deleteNodes}
}

// RetractDeadCloudResourceNodes resolves liveness inside the per-uid
// lock-holding transaction, releases the dead uids from the ledger, and
// deletes them from the graph. It returns the number of deleted nodes.
func (w *CloudResourceRetracter) RetractDeadCloudResourceNodes(
	ctx context.Context,
	uids []string,
	evidenceSource string,
) (int, error) {
	return w.gate.RetractDeadUIDs(
		ctx, familyCloudResource, uids, evidenceSource,
		postgres.LiveAdmissionCloudUIDs, w.deleteNodes,
	)
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
