// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

func benchEC2BlockDeviceKMSPostureNodeRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"uid":                      fmt.Sprintf("ec2-%d", i),
			"state":                    "encrypted",
			"reason":                   "all_volumes_customer_managed_kms",
			"volume_count":             int64(2),
			"encrypted_volume_count":   int64(2),
			"unencrypted_volume_count": int64(0),
			"unresolved_volume_count":  int64(0),
			"kms_key_count":            int64(1),
			"volume_ids":               []string{fmt.Sprintf("vol-a-%d", i), fmt.Sprintf("vol-b-%d", i)},
			"kms_key_ids":              []string{fmt.Sprintf("arn:aws:kms:us-east-1:111122223333:key/%d", i)},
			"source_fact_id":           fmt.Sprintf("fact-%d", i),
		})
	}
	return rows
}

// BenchmarkEC2BlockDeviceKMSPostureNodeWriter measures the Eshu-owned
// statement-construction and batching cost for the EC2 block-device KMS posture
// node-property writer. The backend executor is a no-op, so the benchmark
// isolates the batched uid-anchored MATCH+SET path from graph round trips.
func BenchmarkEC2BlockDeviceKMSPostureNodeWriter(b *testing.B) {
	rows := benchEC2BlockDeviceKMSPostureNodeRows(5000)
	writer := NewEC2BlockDeviceKMSPostureNodeWriter(noopGroupExecutor{}, &echoingPostureExistenceReader{}, 500)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writer.WriteEC2BlockDeviceKMSPostureNodes(ctx, rows, "scope-1", "gen-1", "reducer/ec2-block-device-kms-posture"); err != nil {
			b.Fatalf("WriteEC2BlockDeviceKMSPostureNodes: %v", err)
		}
	}
}
