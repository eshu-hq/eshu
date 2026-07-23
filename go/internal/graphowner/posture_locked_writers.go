// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import "context"

// Family names identify the owning writer in lock-only error context, mirroring
// family_writers.go's owner-ledger family constants.
const (
	familyRDSPosture          = "rds_posture"
	familyEC2InternetExposure = "ec2_internet_exposure"
	familyEC2BlockDeviceKMS   = "ec2_block_device_kms_posture"
	familyS3InternetExposure  = "s3_internet_exposure"
	familyEC2InstanceIdentity = "ec2_instance_identity"
)

// RDSPostureLockedWriter wraps the RDS posture CloudResource property writer
// with the #5062 lock-only gate. It satisfies the reducer's
// RDSPostureNodeWriter consumer interface.
type RDSPostureLockedWriter struct {
	gate    *LockOnlyGate
	write   postureNodeWriteFunc
	retract postureNodeRetractFunc
}

// NewRDSPostureLockedWriter gates write (the raw cypher writer's
// WriteRDSPostureNodes method) on gate's per-uid advisory lock. retract (the
// raw writer's RetractRDSPostureNodes method) is forwarded unchanged — see the
// LockOnlyGate doc comment for why retraction is not lock-gated.
func NewRDSPostureLockedWriter(gate *LockOnlyGate, write postureNodeWriteFunc, retract postureNodeRetractFunc) *RDSPostureLockedWriter {
	return &RDSPostureLockedWriter{gate: gate, write: write, retract: retract}
}

// WriteRDSPostureNodes locks the batch's uids, then writes the RDS posture
// rows to the graph while those locks are held.
func (w *RDSPostureLockedWriter) WriteRDSPostureNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error {
	return w.gate.write(ctx, familyRDSPosture, rows, scopeID, generationID, evidenceSource, w.write)
}

// RetractRDSPostureNodes forwards to the underlying writer's retract
// unchanged; see the LockOnlyGate doc comment.
func (w *RDSPostureLockedWriter) RetractRDSPostureNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error {
	return w.retract(ctx, scopeIDs, generationID, evidenceSource)
}

// EC2InternetExposureLockedWriter wraps the EC2 internet-exposure CloudResource
// property writer with the #5062 lock-only gate. It satisfies the reducer's
// EC2InternetExposureNodeWriter consumer interface.
type EC2InternetExposureLockedWriter struct {
	gate    *LockOnlyGate
	write   postureNodeWriteFunc
	retract postureNodeRetractFunc
}

// NewEC2InternetExposureLockedWriter gates write (the raw cypher writer's
// WriteEC2InternetExposureNodes method) on gate's per-uid advisory lock.
// retract is forwarded unchanged; see the LockOnlyGate doc comment.
func NewEC2InternetExposureLockedWriter(gate *LockOnlyGate, write postureNodeWriteFunc, retract postureNodeRetractFunc) *EC2InternetExposureLockedWriter {
	return &EC2InternetExposureLockedWriter{gate: gate, write: write, retract: retract}
}

// WriteEC2InternetExposureNodes locks the batch's uids, then writes the EC2
// internet-exposure rows to the graph while those locks are held.
func (w *EC2InternetExposureLockedWriter) WriteEC2InternetExposureNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error {
	return w.gate.write(ctx, familyEC2InternetExposure, rows, scopeID, generationID, evidenceSource, w.write)
}

// RetractEC2InternetExposureNodes forwards to the underlying writer's retract
// unchanged; see the LockOnlyGate doc comment.
func (w *EC2InternetExposureLockedWriter) RetractEC2InternetExposureNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error {
	return w.retract(ctx, scopeIDs, generationID, evidenceSource)
}

// EC2BlockDeviceKMSPostureLockedWriter wraps the EC2 block-device KMS posture
// CloudResource property writer with the #5062 lock-only gate. It satisfies the
// reducer's EC2BlockDeviceKMSPostureNodeWriter consumer interface.
type EC2BlockDeviceKMSPostureLockedWriter struct {
	gate    *LockOnlyGate
	write   postureNodeWriteFunc
	retract postureNodeRetractFunc
}

// NewEC2BlockDeviceKMSPostureLockedWriter gates write (the raw cypher writer's
// WriteEC2BlockDeviceKMSPostureNodes method) on gate's per-uid advisory lock.
// retract is forwarded unchanged; see the LockOnlyGate doc comment.
func NewEC2BlockDeviceKMSPostureLockedWriter(gate *LockOnlyGate, write postureNodeWriteFunc, retract postureNodeRetractFunc) *EC2BlockDeviceKMSPostureLockedWriter {
	return &EC2BlockDeviceKMSPostureLockedWriter{gate: gate, write: write, retract: retract}
}

// WriteEC2BlockDeviceKMSPostureNodes locks the batch's uids, then writes the
// EC2 block-device KMS posture rows to the graph while those locks are held.
func (w *EC2BlockDeviceKMSPostureLockedWriter) WriteEC2BlockDeviceKMSPostureNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error {
	return w.gate.write(ctx, familyEC2BlockDeviceKMS, rows, scopeID, generationID, evidenceSource, w.write)
}

// RetractEC2BlockDeviceKMSPostureNodes forwards to the underlying writer's
// retract unchanged; see the LockOnlyGate doc comment.
func (w *EC2BlockDeviceKMSPostureLockedWriter) RetractEC2BlockDeviceKMSPostureNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error {
	return w.retract(ctx, scopeIDs, generationID, evidenceSource)
}

// S3InternetExposureLockedWriter wraps the S3 internet-exposure CloudResource
// property writer with the #5062 lock-only gate. It satisfies the reducer's
// S3InternetExposureNodeWriter consumer interface.
type S3InternetExposureLockedWriter struct {
	gate    *LockOnlyGate
	write   postureNodeWriteFunc
	retract postureNodeRetractFunc
}

// NewS3InternetExposureLockedWriter gates write (the raw cypher writer's
// WriteS3InternetExposureNodes method) on gate's per-uid advisory lock.
// retract is forwarded unchanged; see the LockOnlyGate doc comment.
func NewS3InternetExposureLockedWriter(gate *LockOnlyGate, write postureNodeWriteFunc, retract postureNodeRetractFunc) *S3InternetExposureLockedWriter {
	return &S3InternetExposureLockedWriter{gate: gate, write: write, retract: retract}
}

// WriteS3InternetExposureNodes locks the batch's uids, then writes the S3
// internet-exposure rows to the graph while those locks are held.
func (w *S3InternetExposureLockedWriter) WriteS3InternetExposureNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error {
	return w.gate.write(ctx, familyS3InternetExposure, rows, scopeID, generationID, evidenceSource, w.write)
}

// RetractS3InternetExposureNodes forwards to the underlying writer's retract
// unchanged; see the LockOnlyGate doc comment.
func (w *S3InternetExposureLockedWriter) RetractS3InternetExposureNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error {
	return w.retract(ctx, scopeIDs, generationID, evidenceSource)
}

// EC2InstanceIdentityLockedWriter wraps the #5448 EC2 instance identity
// CloudResource property writer (ami_id) with the #5062 lock-only gate. It
// satisfies the reducer's EC2InstanceIdentityNodeWriter consumer interface.
//
// Like the four sibling posture/exposure writers above, this writer SETs an
// unconditional, disjoint property on a CloudResource node the #5007 owner
// ledger's EC2InstanceGatedWriter (ec2_instance_node_writer.go, wrapped by
// gated_writer.go's EC2InstanceGatedWriter) also writes to for the SAME EC2
// instance uid. It is not itself a cross-scope contributor racing for
// ownership of the node's identity — every scope observes the same launch AMI
// for the same instance — so LockOnlyGate (not the full owner-ledger Gate) is
// the correct primitive: it serializes this writer's per-uid critical section
// against the owner-ledger gate's per-uid critical section on the identical
// advisory-lock key, avoiding the NornicDB OCC abort-retry churn two
// concurrent same-uid writers would otherwise hit (see the LockOnlyGate doc
// comment for the measured churn and why this is a performance primitive, not
// a data-loss fix — the two writers' SET clauses are already proven disjoint
// by TestEC2InstanceIdentityWriterDisjointFromEC2InstancePostureWriter in
// go/internal/storage/cypher, so there is no property to lose either way).
type EC2InstanceIdentityLockedWriter struct {
	gate    *LockOnlyGate
	write   postureNodeWriteFunc
	retract postureNodeRetractFunc
}

// NewEC2InstanceIdentityLockedWriter gates write (the raw cypher writer's
// WriteEC2InstanceIdentityNodes method) on gate's per-uid advisory lock.
// retract is forwarded unchanged; see the LockOnlyGate doc comment.
func NewEC2InstanceIdentityLockedWriter(gate *LockOnlyGate, write postureNodeWriteFunc, retract postureNodeRetractFunc) *EC2InstanceIdentityLockedWriter {
	return &EC2InstanceIdentityLockedWriter{gate: gate, write: write, retract: retract}
}

// WriteEC2InstanceIdentityNodes locks the batch's uids, then writes the EC2
// instance identity rows to the graph while those locks are held.
func (w *EC2InstanceIdentityLockedWriter) WriteEC2InstanceIdentityNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error {
	return w.gate.write(ctx, familyEC2InstanceIdentity, rows, scopeID, generationID, evidenceSource, w.write)
}

// RetractEC2InstanceIdentityNodes forwards to the underlying writer's retract
// unchanged; see the LockOnlyGate doc comment.
func (w *EC2InstanceIdentityLockedWriter) RetractEC2InstanceIdentityNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error {
	return w.retract(ctx, scopeIDs, generationID, evidenceSource)
}
