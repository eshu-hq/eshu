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
