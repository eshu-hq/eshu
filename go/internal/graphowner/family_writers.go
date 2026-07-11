// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import "context"

// Family names identify the owning writer in the cross-scope contention log.
const (
	familyCloudResource      = "cloud_resource"
	familyEC2Instance        = "ec2_instance"
	familyKubernetesWorkload = "kubernetes_workload"
)

// CloudResourceGatedWriter wraps the canonical CloudResource node writer
// (AWS/GCP/Azure share it) with the #5007 owner-ledger gate. It satisfies the
// reducer's CloudResourceNodeWriter consumer interface.
type CloudResourceGatedWriter struct {
	gate  *Gate
	inner nodeBatchWriteFunc
}

// NewCloudResourceGatedWriter gates inner (the raw cypher writer's
// WriteCloudResourceNodes method) on the owner ledger via gate.
func NewCloudResourceGatedWriter(gate *Gate, inner nodeBatchWriteFunc) *CloudResourceGatedWriter {
	return &CloudResourceGatedWriter{gate: gate, inner: inner}
}

// WriteCloudResourceNodes resolves cross-scope ownership, then writes the owned
// CloudResource rows to the graph under the per-uid advisory locks.
func (w *CloudResourceGatedWriter) WriteCloudResourceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error {
	return w.gate.write(ctx, familyCloudResource, rows, evidenceSource, w.inner)
}

// EC2InstanceGatedWriter wraps the EC2 instance CloudResource node writer with
// the #5007 owner-ledger gate. It satisfies the reducer's EC2InstanceNodeWriter
// consumer interface.
type EC2InstanceGatedWriter struct {
	gate  *Gate
	inner nodeBatchWriteFunc
}

// NewEC2InstanceGatedWriter gates inner (the raw cypher writer's
// WriteEC2InstanceNodes method) on the owner ledger via gate.
func NewEC2InstanceGatedWriter(gate *Gate, inner nodeBatchWriteFunc) *EC2InstanceGatedWriter {
	return &EC2InstanceGatedWriter{gate: gate, inner: inner}
}

// WriteEC2InstanceNodes resolves cross-scope ownership, then writes the owned
// EC2 instance rows to the graph under the per-uid advisory locks.
func (w *EC2InstanceGatedWriter) WriteEC2InstanceNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error {
	return w.gate.write(ctx, familyEC2Instance, rows, evidenceSource, w.inner)
}

// KubernetesWorkloadGatedWriter wraps the KubernetesWorkload node writer with
// the #5007 owner-ledger gate. It satisfies the reducer's
// KubernetesWorkloadNodeWriter consumer interface.
type KubernetesWorkloadGatedWriter struct {
	gate  *Gate
	inner nodeBatchWriteFunc
}

// NewKubernetesWorkloadGatedWriter gates inner (the raw cypher writer's
// WriteKubernetesWorkloadNodes method) on the owner ledger via gate.
func NewKubernetesWorkloadGatedWriter(gate *Gate, inner nodeBatchWriteFunc) *KubernetesWorkloadGatedWriter {
	return &KubernetesWorkloadGatedWriter{gate: gate, inner: inner}
}

// WriteKubernetesWorkloadNodes resolves cross-scope ownership, then writes the
// owned KubernetesWorkload rows to the graph under the per-uid advisory locks.
func (w *KubernetesWorkloadGatedWriter) WriteKubernetesWorkloadNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error {
	return w.gate.write(ctx, familyKubernetesWorkload, rows, evidenceSource, w.inner)
}
