// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// boltPostureExistenceReader adapts the bolt test runner to
// PostureExistenceReader so the live posture-writer tests exercise the exact
// production read+write seam (issue #5652), not a hand-rolled mock.
type boltPostureExistenceReader struct {
	runner *boltRetractTestRunner
}

func (r *boltPostureExistenceReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.runner.runCypher(ctx, cypher, params)
}

// TestLivePostureNodeWritersPersistAndNeverCreate is the committed live
// discriminating regression for issue #5652: on the pinned production
// NornicDB image (nornicdb-cpu-bge:v1.1.11), a bare-MATCH-anchored UNWIND SET
// silently drops its write. It drives each of the four production posture
// node writers (EC2 internet exposure, EC2 block-device KMS posture, RDS
// posture, S3 internet exposure) through the real Bolt driver and proves, by
// read-back in a separate transaction:
//
//  1. The write persists for a CloudResource uid the reader confirms exists
//     (the fix: MERGE-anchored SET, filtered by a prior existence read).
//  2. A row for a uid that does NOT exist is dropped before it ever reaches
//     the MERGE-anchored statement: the CloudResource count is unchanged and
//     no phantom node is created (the never-create contract every posture
//     writer's doc comment states).
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. bolt://127.0.0.1:48687, the
// isolated NornicDB v1.1.11 Compose port used for this investigation). When
// unset the test skips, so the default `go test` run needs no Docker.
func TestLivePostureNodeWritersPersistAndNeverCreate(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()
	reader := &boltPostureExistenceReader{runner: runner}
	executor := &boltTestExecutor{runner: runner}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	cases := []struct {
		name             string
		readBackProperty string
		write            func(ctx context.Context, existingUID, missingUID string) error
	}{
		{
			name:             "ec2_internet_exposure",
			readBackProperty: "ec2_internet_exposure_state",
			write: func(ctx context.Context, existingUID, missingUID string) error {
				w := NewEC2InternetExposureNodeWriter(executor, reader, 0)
				rows := []map[string]any{
					{"uid": existingUID, "state": "exposed", "internet_exposed": true, "reason": "sg-0.0.0.0/0", "source_fact_id": "fact-1"},
					{"uid": missingUID, "state": "exposed", "internet_exposed": true, "reason": "sg-0.0.0.0/0", "source_fact_id": "fact-2"},
				}
				return w.WriteEC2InternetExposureNodes(ctx, rows, "scope-5652", "gen-5652", "reducer/ec2-internet-exposure")
			},
		},
		{
			name:             "ec2_block_device_kms_posture",
			readBackProperty: "ec2_block_device_kms_state",
			write: func(ctx context.Context, existingUID, missingUID string) error {
				w := NewEC2BlockDeviceKMSPostureNodeWriter(executor, reader, 0)
				row := func(uid string) map[string]any {
					return map[string]any{
						"uid": uid, "state": "unencrypted", "reason": "volume-not-encrypted",
						"volume_count": int64(1), "encrypted_volume_count": int64(0),
						"unencrypted_volume_count": int64(1), "unresolved_volume_count": int64(0),
						"kms_key_count": int64(0), "volume_ids": []string{"vol-1"}, "kms_key_ids": []string{},
						"source_fact_id": "fact-1",
					}
				}
				return w.WriteEC2BlockDeviceKMSPostureNodes(ctx, []map[string]any{row(existingUID), row(missingUID)}, "scope-5652", "gen-5652", "reducer/ec2-block-device-kms-posture")
			},
		},
		{
			name:             "rds_posture",
			readBackProperty: "rds_public_exposure_state",
			write: func(ctx context.Context, existingUID, missingUID string) error {
				w := NewRDSPostureNodeWriter(executor, reader, 0)
				row := func(uid string) map[string]any {
					return map[string]any{
						"uid": uid, "rds_identifier": "db-1", "rds_resource_type": "instance",
						"rds_engine": "postgres", "rds_publicly_accessible": true,
						"rds_public_exposure_state": "exposed", "rds_storage_encrypted": false,
						"rds_kms_key_id": nil, "rds_iam_database_authentication_enabled": false,
						"rds_multi_az": false, "rds_deletion_protection": false,
						"rds_backup_retention_period": int64(7), "rds_performance_insights_enabled": false,
						"rds_performance_insights_retention_days": nil, "rds_performance_insights_kms_key_id": nil,
						"rds_ca_certificate_identifier": "rds-ca-2019", "rds_parameter_groups": []string{"default"},
						"rds_option_groups": []string{}, "rds_security_parameters": []string{},
						"source_fact_id": "fact-1",
					}
				}
				return w.WriteRDSPostureNodes(ctx, []map[string]any{row(existingUID), row(missingUID)}, "scope-5652", "gen-5652", "reducer/rds-posture")
			},
		},
		{
			name:             "s3_internet_exposure",
			readBackProperty: "s3_internet_exposure_state",
			write: func(ctx context.Context, existingUID, missingUID string) error {
				w := NewS3InternetExposureNodeWriter(executor, reader, 0)
				rows := []map[string]any{
					{"uid": existingUID, "state": "exposed", "internet_exposed": true, "reason": "bucket-policy-public", "source_fact_id": "fact-1"},
					{"uid": missingUID, "state": "exposed", "internet_exposed": true, "reason": "bucket-policy-public", "source_fact_id": "fact-2"},
				}
				return w.WriteS3InternetExposureNodes(ctx, rows, "scope-5652", "gen-5652", "reducer/s3-internet-exposure")
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			existingUID := fmt.Sprintf("5652-%s-existing-%s", tc.name, suffix)
			missingUID := fmt.Sprintf("5652-%s-missing-%s", tc.name, suffix)

			if err := boltWriteStatement(ctx, runner,
				`MERGE (resource:CloudResource {uid: $uid}) SET resource.name = $uid`,
				map[string]any{"uid": existingUID}); err != nil {
				t.Fatalf("seed existing CloudResource: %v", err)
			}

			before, err := boltCount(ctx, runner, `MATCH (n:CloudResource) RETURN count(n) AS count`, nil)
			if err != nil {
				t.Fatalf("count CloudResource before write: %v", err)
			}

			if err := tc.write(ctx, existingUID, missingUID); err != nil {
				t.Fatalf("write posture nodes: %v", err)
			}

			// Assertion 1: the write persisted for the confirmed-existing uid,
			// read back in a SEPARATE transaction.
			rows, err := runner.runCypher(ctx,
				fmt.Sprintf(`MATCH (resource:CloudResource {uid: $uid}) RETURN resource.%s AS value`, tc.readBackProperty),
				map[string]any{"uid": existingUID})
			if err != nil {
				t.Fatalf("read back existing uid: %v", err)
			}
			if len(rows) == 0 || rows[0]["value"] == nil {
				t.Fatalf("%s: property %s did not persist for confirmed-existing uid (read-back = %v) -- "+
					"the bare-MATCH silent no-op regressed", tc.name, tc.readBackProperty, rows)
			}

			// Assertion 2: never-create holds for the missing uid.
			after, err := boltCount(ctx, runner, `MATCH (n:CloudResource) RETURN count(n) AS count`, nil)
			if err != nil {
				t.Fatalf("count CloudResource after write: %v", err)
			}
			if after != before {
				t.Fatalf("%s: CloudResource count changed by %d during the write (want 0; the existing uid "+
					"was already seeded before this count was taken), so the missing uid's row created a "+
					"phantom node -- never-create violated", tc.name, after-before)
			}
			missingExists, err := boltCount(ctx, runner, `MATCH (n:CloudResource {uid: $uid}) RETURN count(n) AS count`, map[string]any{"uid": missingUID})
			if err != nil {
				t.Fatalf("check missing uid existence: %v", err)
			}
			if missingExists != 0 {
				t.Fatalf("%s: a phantom CloudResource node was created for the never-confirmed uid %q", tc.name, missingUID)
			}
		})
	}
}
