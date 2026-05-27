package awssdk

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbackup "github.com/aws/aws-sdk-go-v2/service/backup"
	awsbackuptypes "github.com/aws/aws-sdk-go-v2/service/backup/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListBackupVaultsProjectsMetadataAndExcludesAccessPolicy(t *testing.T) {
	vaultARN := "arn:aws:backup:us-east-1:123456789012:backup-vault:prod"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/abcd"
	fake := &fakeBackupAPI{
		listBackupVaults: []*awsbackup.ListBackupVaultsOutput{{
			BackupVaultList: []awsbackuptypes.BackupVaultListMember{{
				BackupVaultArn:         aws.String(vaultARN),
				BackupVaultName:        aws.String("prod"),
				EncryptionKeyArn:       aws.String(kmsARN),
				NumberOfRecoveryPoints: 7,
				Locked:                 aws.Bool(true),
				LockDate:               aws.Time(time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)),
				CreationDate:           aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
			}},
		}},
		describeBackupVaults: map[string]*awsbackup.DescribeBackupVaultOutput{
			"prod": {
				BackupVaultArn:    aws.String(vaultARN),
				BackupVaultName:   aws.String("prod"),
				EncryptionKeyType: awsbackuptypes.EncryptionKeyType("CUSTOMER_MANAGED_KMS_KEY"),
				Locked:            aws.Bool(true),
				MinRetentionDays:  aws.Int64(30),
				MaxRetentionDays:  aws.Int64(365),
			},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceBackup},
	}
	vaults, err := adapter.ListBackupVaults(context.Background())
	if err != nil {
		t.Fatalf("ListBackupVaults() error = %v", err)
	}
	if got, want := len(vaults), 1; got != want {
		t.Fatalf("len(vaults) = %d, want %d", got, want)
	}
	if vaults[0].EncryptionKeyType != "CUSTOMER_MANAGED_KMS_KEY" {
		t.Fatalf("encryption_key_type = %q, want CUSTOMER_MANAGED_KMS_KEY", vaults[0].EncryptionKeyType)
	}
	if vaults[0].MinRetentionDays == nil || *vaults[0].MinRetentionDays != 30 {
		t.Fatalf("MinRetentionDays = %v, want 30", vaults[0].MinRetentionDays)
	}
	if fake.getBackupVaultAccessPolicyCalls != 0 {
		t.Fatalf("GetBackupVaultAccessPolicy was called %d times; adapter must never call it",
			fake.getBackupVaultAccessPolicyCalls)
	}
}

func TestClientListBackupSelectionsMergesTagConditions(t *testing.T) {
	planID := "plan-1"
	roleARN := "arn:aws:iam::123456789012:role/backup"
	resourceARN := "arn:aws:rds:us-east-1:123456789012:db:prod-db"
	fake := &fakeBackupAPI{
		listBackupSelections: []*awsbackup.ListBackupSelectionsOutput{{
			BackupSelectionsList: []awsbackuptypes.BackupSelectionsListMember{{
				BackupPlanId:  aws.String(planID),
				SelectionId:   aws.String("sel-1"),
				SelectionName: aws.String("all-rds"),
				IamRoleArn:    aws.String(roleARN),
				CreationDate:  aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
			}},
		}},
		getBackupSelections: map[string]*awsbackup.GetBackupSelectionOutput{
			"sel-1": {
				BackupSelection: &awsbackuptypes.BackupSelection{
					IamRoleArn:    aws.String(roleARN),
					SelectionName: aws.String("all-rds"),
					Resources:     []string{resourceARN},
					ListOfTags: []awsbackuptypes.Condition{{
						ConditionType:  awsbackuptypes.ConditionType("STRINGEQUALS"),
						ConditionKey:   aws.String("aws:ResourceTag/backup"),
						ConditionValue: aws.String("daily"),
					}},
					Conditions: &awsbackuptypes.Conditions{
						StringEquals: []awsbackuptypes.ConditionParameter{{
							ConditionKey:   aws.String("aws:ResourceTag/team"),
							ConditionValue: aws.String("payments"),
						}},
					},
				},
			},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceBackup},
	}
	selections, err := adapter.ListBackupSelections(context.Background(), planID)
	if err != nil {
		t.Fatalf("ListBackupSelections() error = %v", err)
	}
	if got, want := len(selections), 1; got != want {
		t.Fatalf("len(selections) = %d, want %d", got, want)
	}
	sel := selections[0]
	if got, want := sel.Resources, []string{resourceARN}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Resources = %#v, want %#v", got, want)
	}
	if got, want := len(sel.TagConditions), 2; got != want {
		t.Fatalf("len(TagConditions) = %d, want %d", got, want)
	}
}

func TestClientListRecoveryPointsExcludesRestoreMetadata(t *testing.T) {
	rpARN := "arn:aws:backup:us-east-1:123456789012:recovery-point:rp-1"
	fake := &fakeBackupAPI{
		listRecoveryPoints: map[string][]*awsbackup.ListRecoveryPointsByBackupVaultOutput{
			"prod": {{
				RecoveryPoints: []awsbackuptypes.RecoveryPointByBackupVault{{
					RecoveryPointArn:  aws.String(rpARN),
					BackupVaultName:   aws.String("prod"),
					BackupVaultArn:    aws.String("arn:aws:backup:us-east-1:123456789012:backup-vault:prod"),
					ResourceArn:       aws.String("arn:aws:rds:us-east-1:123456789012:db:prod"),
					ResourceType:      aws.String("RDS"),
					Status:            awsbackuptypes.RecoveryPointStatusCompleted,
					IsEncrypted:       true,
					CreationDate:      aws.Time(time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)),
					CompletionDate:    aws.Time(time.Date(2026, 5, 20, 0, 30, 0, 0, time.UTC)),
					BackupSizeInBytes: aws.Int64(2048),
					CalculatedLifecycle: &awsbackuptypes.CalculatedLifecycle{
						DeleteAt: aws.Time(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)),
					},
				}},
			}},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceBackup},
	}
	rps, err := adapter.ListRecoveryPoints(context.Background(), "prod")
	if err != nil {
		t.Fatalf("ListRecoveryPoints() error = %v", err)
	}
	if got, want := len(rps), 1; got != want {
		t.Fatalf("len(rps) = %d, want %d", got, want)
	}
	rp := rps[0]
	if rp.SourceResourceARN != "arn:aws:rds:us-east-1:123456789012:db:prod" {
		t.Fatalf("SourceResourceARN = %q", rp.SourceResourceARN)
	}
	if rp.BackupSizeInBytes == nil || *rp.BackupSizeInBytes != 2048 {
		t.Fatalf("BackupSizeInBytes = %v, want 2048", rp.BackupSizeInBytes)
	}
	if fake.getRecoveryPointRestoreMetadataCalls != 0 {
		t.Fatalf("GetRecoveryPointRestoreMetadata called %d times; adapter must never read restore metadata",
			fake.getRecoveryPointRestoreMetadataCalls)
	}
	if fake.deleteRecoveryPointCalls != 0 {
		t.Fatalf("DeleteRecoveryPoint called %d times; adapter must never delete recovery points",
			fake.deleteRecoveryPointCalls)
	}
}

func TestClientListFrameworksProjectsControlSummaryWithoutInputParameters(t *testing.T) {
	frameworkARN := "arn:aws:backup:us-east-1:123456789012:framework:fw-1"
	fake := &fakeBackupAPI{
		listFrameworks: []*awsbackup.ListFrameworksOutput{{
			Frameworks: []awsbackuptypes.Framework{{
				FrameworkArn:     aws.String(frameworkARN),
				FrameworkName:    aws.String("fw-1"),
				DeploymentStatus: aws.String("COMPLETED"),
				NumberOfControls: 1,
				CreationTime:     aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
			}},
		}},
		describeFrameworks: map[string]*awsbackup.DescribeFrameworkOutput{
			"fw-1": {
				FrameworkName: aws.String("fw-1"),
				FrameworkControls: []awsbackuptypes.FrameworkControl{{
					ControlName: aws.String("BACKUP_RECOVERY_POINT_MINIMUM_RETENTION_CHECK"),
					ControlInputParameters: []awsbackuptypes.ControlInputParameter{{
						ParameterName:  aws.String("requiredRetentionDays"),
						ParameterValue: aws.String("35"),
					}},
					ControlScope: &awsbackuptypes.ControlScope{
						ComplianceResourceTypes: []string{"BACKUP_PLAN"},
						Tags:                    map[string]string{"Compliance": "SOC2"},
					},
				}},
			},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceBackup},
	}
	frameworks, err := adapter.ListFrameworks(context.Background())
	if err != nil {
		t.Fatalf("ListFrameworks() error = %v", err)
	}
	if got, want := len(frameworks), 1; got != want {
		t.Fatalf("len(frameworks) = %d, want %d", got, want)
	}
	controls := frameworks[0].Controls
	if got, want := len(controls), 1; got != want {
		t.Fatalf("len(controls) = %d, want %d", got, want)
	}
	control := controls[0]
	if got, want := control.Name, "BACKUP_RECOVERY_POINT_MINIMUM_RETENTION_CHECK"; got != want {
		t.Fatalf("control name = %q, want %q", got, want)
	}
	if got, want := control.ScopeComplianceTypes, []string{"BACKUP_PLAN"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scope_compliance_types = %#v, want %#v", got, want)
	}
	if got, want := control.ScopeTagKeys, []string{"Compliance"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scope_tag_keys = %#v, want %#v", got, want)
	}
	// Control input parameter VALUES must never leak through the projection.
	// The mapped struct does not expose a field for them, but assert defensively
	// that no field in the mapped value contains a parameter value.
	for _, forbidden := range []string{"requiredRetentionDays", "35"} {
		if strings.Contains(controlString(control), forbidden) {
			t.Fatalf("framework control projection contains forbidden value %q: %#v", forbidden, control)
		}
	}
}

func TestClientNoMutationOrUnsafeReadAPIsCalled(t *testing.T) {
	fake := &fakeBackupAPI{}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceBackup},
	}
	if _, err := adapter.ListBackupVaults(context.Background()); err != nil {
		t.Fatalf("ListBackupVaults() error = %v", err)
	}
	if _, err := adapter.ListBackupPlans(context.Background()); err != nil {
		t.Fatalf("ListBackupPlans() error = %v", err)
	}
	if _, err := adapter.ListRecoveryPoints(context.Background(), "any"); err != nil {
		t.Fatalf("ListRecoveryPoints() error = %v", err)
	}
	if _, err := adapter.ListReportPlans(context.Background()); err != nil {
		t.Fatalf("ListReportPlans() error = %v", err)
	}
	if _, err := adapter.ListRestoreTestingPlans(context.Background()); err != nil {
		t.Fatalf("ListRestoreTestingPlans() error = %v", err)
	}
	if _, err := adapter.ListFrameworks(context.Background()); err != nil {
		t.Fatalf("ListFrameworks() error = %v", err)
	}
	if fake.totalForbiddenCalls() != 0 {
		t.Fatalf("forbidden API counters non-zero: %+v", fake)
	}
}

func controlString(v any) string {
	return fmt.Sprintf("%#v", v)
}
