// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestScannerEmitsVaultPlanSelectionAndRelationships(t *testing.T) {
	vaultARN := "arn:aws:backup:us-east-1:123456789012:backup-vault:prod-vault"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/abcd1234-12ab-34cd-56ef-1234567890ab"
	planARN := "arn:aws:backup:us-east-1:123456789012:backup-plan:plan-id"
	roleARN := "arn:aws:iam::123456789012:role/backup-role"
	includedResource := "arn:aws:dynamodb:us-east-1:123456789012:table/users"

	client := fakeClient{
		vaults: []Vault{{
			ARN:                    vaultARN,
			Name:                   "prod-vault",
			EncryptionKeyARN:       kmsARN,
			EncryptionKeyType:      "CUSTOMER_MANAGED_KMS_KEY",
			Locked:                 true,
			LockDate:               time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
			NumberOfRecoveryPoints: 42,
			CreationDate:           time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			HasAccessPolicy:        true,
		}},
		plans: []Plan{{
			ARN:               planARN,
			ID:                "plan-id",
			Name:              "daily-plan",
			VersionID:         "v1",
			CreationDate:      time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			LastExecutionDate: time.Date(2026, 5, 27, 5, 0, 0, 0, time.UTC),
			Rules: []PlanRule{{
				Name:                  "daily",
				ScheduleExpression:    "cron(0 5 ? * * *)",
				TargetBackupVaultName: "prod-vault",
			}},
		}},
		selections: map[string][]Selection{
			"plan-id": {{
				ID:           "sel-1",
				Name:         "all-dynamodb",
				PlanID:       "plan-id",
				IAMRoleARN:   roleARN,
				Resources:    []string{includedResource},
				CreationDate: time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC),
				TagConditions: []TagCondition{{
					Operator: "StringEquals",
					Key:      "aws:ResourceTag/backup",
					Value:    "daily",
				}},
			}},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	vault := resourceByType(t, envelopes, awscloud.ResourceTypeBackupVault)
	vaultAttrs := attributesOf(t, vault)
	if got, want := vaultAttrs["encryption_key_arn"], kmsARN; got != want {
		t.Fatalf("vault encryption_key_arn = %#v, want %q", got, want)
	}
	if got, want := vaultAttrs["locked"], true; got != want {
		t.Fatalf("vault locked = %#v, want %v", got, want)
	}
	if got, want := vaultAttrs["number_of_recovery_points"], int64(42); got != want {
		t.Fatalf("vault number_of_recovery_points = %#v, want %d", got, want)
	}
	if got, want := vaultAttrs["has_access_policy"], true; got != want {
		t.Fatalf("vault has_access_policy = %#v, want %v", got, want)
	}
	// Critical security invariant: the scanner must never persist the access
	// policy JSON body, statements, or principals.
	for _, forbidden := range []string{"access_policy", "access_policy_document", "policy", "policy_document", "statements", "principals"} {
		if _, exists := vaultAttrs[forbidden]; exists {
			t.Fatalf("vault attribute %q persisted; scanner must never persist vault access policy body", forbidden)
		}
	}

	plan := resourceByType(t, envelopes, awscloud.ResourceTypeBackupPlan)
	planAttrs := attributesOf(t, plan)
	if got, want := planAttrs["version_id"], "v1"; got != want {
		t.Fatalf("plan version_id = %#v, want %q", got, want)
	}
	if got, want := planAttrs["plan_id"], "plan-id"; got != want {
		t.Fatalf("plan plan_id = %#v, want %q", got, want)
	}
	rules, ok := planAttrs["rules"].([]map[string]any)
	if !ok {
		t.Fatalf("plan rules attribute = %#v, want []map[string]any", planAttrs["rules"])
	}
	if got, want := len(rules), 1; got != want {
		t.Fatalf("len(plan.rules) = %d, want %d", got, want)
	}
	if got, want := rules[0]["schedule_expression"], "cron(0 5 ? * * *)"; got != want {
		t.Fatalf("rule schedule_expression = %#v, want %q", got, want)
	}

	selection := resourceByType(t, envelopes, awscloud.ResourceTypeBackupSelection)
	selAttrs := attributesOf(t, selection)
	if got, want := selAttrs["iam_role_arn"], roleARN; got != want {
		t.Fatalf("selection iam_role_arn = %#v, want %q", got, want)
	}
	if got, want := selAttrs["resources"], []string{includedResource}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selection resources = %#v, want %#v", got, want)
	}
	tagConds, ok := selAttrs["tag_conditions"].([]map[string]any)
	if !ok {
		t.Fatalf("selection tag_conditions = %#v, want []map[string]any", selAttrs["tag_conditions"])
	}
	if got, want := tagConds[0]["operator"], "StringEquals"; got != want {
		t.Fatalf("tag_conditions[0].operator = %#v, want %q", got, want)
	}
	if got, want := tagConds[0]["key"], "aws:ResourceTag/backup"; got != want {
		t.Fatalf("tag_conditions[0].key = %#v, want %q", got, want)
	}

	planSelection := relationshipByType(t, envelopes, awscloud.RelationshipBackupPlanHasSelection)
	if got, want := planSelection.Payload["source_arn"], planARN; got != want {
		t.Fatalf("plan-selection source_arn = %#v, want %q", got, want)
	}
	if got, want := planSelection.Payload["target_resource_id"], "sel-1"; got != want {
		t.Fatalf("plan-selection target_resource_id = %#v, want %q", got, want)
	}

	selResource := relationshipByType(t, envelopes, awscloud.RelationshipBackupSelectionIncludesResource)
	if got, want := selResource.Payload["target_arn"], includedResource; got != want {
		t.Fatalf("selection-resource target_arn = %#v, want %q", got, want)
	}
	if got, want := selResource.Payload["target_type"], awscloud.ResourceTypeDynamoDBTable; got != want {
		t.Fatalf("selection-resource target_type = %#v, want %q", got, want)
	}

	selRole := relationshipByType(t, envelopes, awscloud.RelationshipBackupSelectionUsesIAMRole)
	if got, want := selRole.Payload["target_arn"], roleARN; got != want {
		t.Fatalf("selection-role target_arn = %#v, want %q", got, want)
	}

	vaultKMS := relationshipByType(t, envelopes, awscloud.RelationshipBackupVaultUsesKMSKey)
	if got, want := vaultKMS.Payload["target_arn"], kmsARN; got != want {
		t.Fatalf("vault-kms target_arn = %#v, want %q", got, want)
	}
}

func TestScannerEmitsRecoveryPointMetadataOnly(t *testing.T) {
	rpARN := "arn:aws:backup:us-east-1:123456789012:recovery-point:rp-abc123"
	vaultARN := "arn:aws:backup:us-east-1:123456789012:backup-vault:prod-vault"
	sourceARN := "arn:aws:rds:us-east-1:123456789012:db:prod-db"
	rpCreation := time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC)
	rpExpiration := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)

	client := fakeClient{
		vaults: []Vault{{ARN: vaultARN, Name: "prod-vault"}},
		recoveryPoints: map[string][]RecoveryPoint{
			"prod-vault": {{
				ARN:                rpARN,
				VaultName:          "prod-vault",
				VaultARN:           vaultARN,
				SourceResourceARN:  sourceARN,
				SourceResourceType: "RDS",
				Status:             "COMPLETED",
				IsEncrypted:        true,
				CreationDate:       rpCreation,
				CalculatedDeleteAt: rpExpiration,
				BackupSizeInBytes:  int64Ptr(1024),
				StorageClass:       "WARM",
			}},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	rp := resourceByType(t, envelopes, awscloud.ResourceTypeBackupRecoveryPoint)
	attrs := attributesOf(t, rp)
	if got, want := attrs["source_resource_arn"], sourceARN; got != want {
		t.Fatalf("recovery-point source_resource_arn = %#v, want %q", got, want)
	}
	if got, want := attrs["status"], "COMPLETED"; got != want {
		t.Fatalf("recovery-point status = %#v, want %q", got, want)
	}
	if got, want := attrs["storage_class"], "WARM"; got != want {
		t.Fatalf("recovery-point storage_class = %#v, want %q", got, want)
	}
	if got, want := attrs["backup_size_in_bytes"], int64(1024); got != want {
		t.Fatalf("recovery-point backup_size_in_bytes = %#v, want %d", got, want)
	}
	// Snapshot content and restore metadata values must never reach the
	// persisted attribute set.
	for _, forbidden := range []string{
		"snapshot",
		"snapshot_contents",
		"contents",
		"restore_metadata",
		"recovery_point_restore_metadata",
	} {
		if _, exists := attrs[forbidden]; exists {
			t.Fatalf("recovery point attribute %q persisted; scanner must never read snapshot or restore metadata", forbidden)
		}
	}

	inVault := relationshipByType(t, envelopes, awscloud.RelationshipBackupRecoveryPointInVault)
	if got, want := inVault.Payload["target_arn"], vaultARN; got != want {
		t.Fatalf("recovery-point in-vault target_arn = %#v, want %q", got, want)
	}

	ofResource := relationshipByType(t, envelopes, awscloud.RelationshipBackupRecoveryPointOfResource)
	if got, want := ofResource.Payload["target_arn"], sourceARN; got != want {
		t.Fatalf("recovery-point of-resource target_arn = %#v, want %q", got, want)
	}
	if got, want := ofResource.Payload["target_type"], awscloud.ResourceTypeRDSDBInstance; got != want {
		t.Fatalf("recovery-point of-resource target_type = %#v, want %q", got, want)
	}
}

func TestScannerEmitsReportPlanRestoreTestingFrameworkMetadata(t *testing.T) {
	reportARN := "arn:aws:backup:us-east-1:123456789012:report-plan:weekly"
	restoreARN := "arn:aws:backup:us-east-1:123456789012:restore-testing-plan:rt-test"
	frameworkARN := "arn:aws:backup:us-east-1:123456789012:framework:fw-1"

	client := fakeClient{
		reportPlans: []ReportPlan{{
			ARN:              reportARN,
			Name:             "weekly",
			DeploymentStatus: "COMPLETED",
			ReportTemplate:   "BACKUP_JOB_REPORT",
			Formats:          []string{"CSV"},
			S3BucketName:     "backup-reports",
			S3KeyPrefix:      "weekly/",
			CreationTime:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		}},
		restoreTestingPlans: []RestoreTestingPlan{{
			ARN:                restoreARN,
			Name:               "rt-test",
			ScheduleExpression: "cron(0 6 ? * * *)",
			ScheduleTimezone:   "UTC",
			CreationTime:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		}},
		frameworks: []Framework{{
			ARN:              frameworkARN,
			Name:             "fw-1",
			Description:      "Backup framework",
			DeploymentStatus: "COMPLETED",
			NumberOfControls: 3,
			CreationTime:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Controls: []FrameworkControl{{
				Name:                 "BACKUP_RECOVERY_POINT_MINIMUM_RETENTION_CHECK",
				ScopeComplianceTypes: []string{"BACKUP_PLAN"},
			}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	report := resourceByType(t, envelopes, awscloud.ResourceTypeBackupReportPlan)
	rAttrs := attributesOf(t, report)
	if got, want := rAttrs["s3_bucket_name"], "backup-reports"; got != want {
		t.Fatalf("report s3_bucket_name = %#v, want %q", got, want)
	}
	if got, want := rAttrs["report_template"], "BACKUP_JOB_REPORT"; got != want {
		t.Fatalf("report report_template = %#v, want %q", got, want)
	}

	restore := resourceByType(t, envelopes, awscloud.ResourceTypeBackupRestoreTestingPlan)
	rtAttrs := attributesOf(t, restore)
	if got, want := rtAttrs["schedule_expression"], "cron(0 6 ? * * *)"; got != want {
		t.Fatalf("restore-testing schedule_expression = %#v, want %q", got, want)
	}

	framework := resourceByType(t, envelopes, awscloud.ResourceTypeBackupFramework)
	fAttrs := attributesOf(t, framework)
	if got, want := fAttrs["number_of_controls"], int32(3); got != want {
		t.Fatalf("framework number_of_controls = %#v, want %d", got, want)
	}
	// Framework control input parameter values are compliance-sensitive and
	// must never appear in the persisted attribute set.
	for _, forbidden := range []string{
		"control_input_parameters",
		"input_parameters",
		"parameter_values",
	} {
		if _, exists := fAttrs[forbidden]; exists {
			t.Fatalf("framework attribute %q persisted; scanner must never persist framework control input parameter values", forbidden)
		}
	}

	control := resourceByType(t, envelopes, awscloud.ResourceTypeBackupFrameworkControl)
	cAttrs := attributesOf(t, control)
	if got, want := cAttrs["control_name"], "BACKUP_RECOVERY_POINT_MINIMUM_RETENTION_CHECK"; got != want {
		t.Fatalf("framework-control control_name = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{
		"control_input_parameters",
		"input_parameters",
		"parameter_values",
		"scope_parameters",
	} {
		if _, exists := cAttrs[forbidden]; exists {
			t.Fatalf("framework-control attribute %q persisted; scanner must never persist control input parameter values", forbidden)
		}
	}

	hasControl := relationshipByType(t, envelopes, awscloud.RelationshipBackupFrameworkHasControl)
	if got, want := hasControl.Payload["source_arn"], frameworkARN; got != want {
		t.Fatalf("framework-control relationship source_arn = %#v, want %q", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func TestScannerSkipsRoleAndIncludeWhenIdentitiesMissing(t *testing.T) {
	client := fakeClient{
		plans: []Plan{{ARN: "arn:aws:backup:us-east-1:123456789012:backup-plan:plan-2", ID: "plan-2", Name: "no-selection"}},
		selections: map[string][]Selection{
			"plan-2": {{
				ID:           "sel-2",
				Name:         "no-identities",
				PlanID:       "plan-2",
				Resources:    []string{"   ", "not-an-arn"},
				CreationDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			}},
		},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipBackupSelectionUsesIAMRole); got != 0 {
		t.Fatalf("selection-role relationship count = %d, want 0 when role missing", got)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipBackupSelectionIncludesResource); got != 0 {
		t.Fatalf("selection-resource relationship count = %d, want 0 when only non-ARN values reported", got)
	}
}
