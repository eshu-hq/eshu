// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbackup "github.com/aws/aws-sdk-go-v2/service/backup"
)

// fakeBackupAPI is the in-test stand-in for the AWS Backup SDK client. It
// implements exactly the apiClient surface the adapter calls. The metadata-only
// guarantee that the adapter never reaches a mutation or unsafe-read API is
// enforced at compile time, not by this fake: apiClient lists only the safe
// reads, the Client.client field is typed as apiClient, and the compile-time
// assertion var _ apiClient = (*awsbackup.Client)(nil) in client.go proves the
// real SDK client is reached through that narrow surface only. A regression
// that wired up GetBackupVaultAccessPolicy, GetRecoveryPointRestoreMetadata,
// StartBackupJob, or any Create/Update/Delete call would fail to compile, and
// the reflective method-shape assertions in
// backup.TestClientInterfaceExcludesMutationAndUnsafeReadAPIs would also catch
// any widening of the exposed interface.
type fakeBackupAPI struct {
	listBackupVaults     []*awsbackup.ListBackupVaultsOutput
	listVaultsCalls      int
	describeBackupVaults map[string]*awsbackup.DescribeBackupVaultOutput

	listBackupPlans []*awsbackup.ListBackupPlansOutput
	listPlansCalls  int
	getBackupPlans  map[string]*awsbackup.GetBackupPlanOutput

	listBackupSelections []*awsbackup.ListBackupSelectionsOutput
	listSelectionsCalls  int
	getBackupSelections  map[string]*awsbackup.GetBackupSelectionOutput

	listRecoveryPoints      map[string][]*awsbackup.ListRecoveryPointsByBackupVaultOutput
	listRecoveryPointsCalls map[string]int

	listReportPlans      []*awsbackup.ListReportPlansOutput
	listReportPlansCalls int

	listRestoreTestingPlans      []*awsbackup.ListRestoreTestingPlansOutput
	listRestoreTestingPlansCalls int

	listFrameworks      []*awsbackup.ListFrameworksOutput
	listFrameworksCalls int
	describeFrameworks  map[string]*awsbackup.DescribeFrameworkOutput
}

func (f *fakeBackupAPI) ListBackupVaults(
	_ context.Context,
	_ *awsbackup.ListBackupVaultsInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.ListBackupVaultsOutput, error) {
	if f.listVaultsCalls >= len(f.listBackupVaults) {
		return &awsbackup.ListBackupVaultsOutput{}, nil
	}
	out := f.listBackupVaults[f.listVaultsCalls]
	f.listVaultsCalls++
	return out, nil
}

func (f *fakeBackupAPI) DescribeBackupVault(
	_ context.Context,
	in *awsbackup.DescribeBackupVaultInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.DescribeBackupVaultOutput, error) {
	if f.describeBackupVaults == nil {
		return &awsbackup.DescribeBackupVaultOutput{}, nil
	}
	out, ok := f.describeBackupVaults[aws.ToString(in.BackupVaultName)]
	if !ok {
		return &awsbackup.DescribeBackupVaultOutput{}, nil
	}
	return out, nil
}

func (f *fakeBackupAPI) ListBackupPlans(
	_ context.Context,
	_ *awsbackup.ListBackupPlansInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.ListBackupPlansOutput, error) {
	if f.listPlansCalls >= len(f.listBackupPlans) {
		return &awsbackup.ListBackupPlansOutput{}, nil
	}
	out := f.listBackupPlans[f.listPlansCalls]
	f.listPlansCalls++
	return out, nil
}

func (f *fakeBackupAPI) GetBackupPlan(
	_ context.Context,
	in *awsbackup.GetBackupPlanInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.GetBackupPlanOutput, error) {
	if f.getBackupPlans == nil {
		return &awsbackup.GetBackupPlanOutput{}, nil
	}
	out, ok := f.getBackupPlans[aws.ToString(in.BackupPlanId)]
	if !ok {
		return &awsbackup.GetBackupPlanOutput{}, nil
	}
	return out, nil
}

func (f *fakeBackupAPI) ListBackupSelections(
	_ context.Context,
	_ *awsbackup.ListBackupSelectionsInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.ListBackupSelectionsOutput, error) {
	if f.listSelectionsCalls >= len(f.listBackupSelections) {
		return &awsbackup.ListBackupSelectionsOutput{}, nil
	}
	out := f.listBackupSelections[f.listSelectionsCalls]
	f.listSelectionsCalls++
	return out, nil
}

func (f *fakeBackupAPI) GetBackupSelection(
	_ context.Context,
	in *awsbackup.GetBackupSelectionInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.GetBackupSelectionOutput, error) {
	if f.getBackupSelections == nil {
		return &awsbackup.GetBackupSelectionOutput{}, nil
	}
	out, ok := f.getBackupSelections[aws.ToString(in.SelectionId)]
	if !ok {
		return &awsbackup.GetBackupSelectionOutput{}, nil
	}
	return out, nil
}

func (f *fakeBackupAPI) ListRecoveryPointsByBackupVault(
	_ context.Context,
	in *awsbackup.ListRecoveryPointsByBackupVaultInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.ListRecoveryPointsByBackupVaultOutput, error) {
	if f.listRecoveryPointsCalls == nil {
		f.listRecoveryPointsCalls = map[string]int{}
	}
	vault := aws.ToString(in.BackupVaultName)
	pages := f.listRecoveryPoints[vault]
	idx := f.listRecoveryPointsCalls[vault]
	if idx >= len(pages) {
		return &awsbackup.ListRecoveryPointsByBackupVaultOutput{}, nil
	}
	f.listRecoveryPointsCalls[vault] = idx + 1
	return pages[idx], nil
}

func (f *fakeBackupAPI) ListReportPlans(
	_ context.Context,
	_ *awsbackup.ListReportPlansInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.ListReportPlansOutput, error) {
	if f.listReportPlansCalls >= len(f.listReportPlans) {
		return &awsbackup.ListReportPlansOutput{}, nil
	}
	out := f.listReportPlans[f.listReportPlansCalls]
	f.listReportPlansCalls++
	return out, nil
}

func (f *fakeBackupAPI) ListRestoreTestingPlans(
	_ context.Context,
	_ *awsbackup.ListRestoreTestingPlansInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.ListRestoreTestingPlansOutput, error) {
	if f.listRestoreTestingPlansCalls >= len(f.listRestoreTestingPlans) {
		return &awsbackup.ListRestoreTestingPlansOutput{}, nil
	}
	out := f.listRestoreTestingPlans[f.listRestoreTestingPlansCalls]
	f.listRestoreTestingPlansCalls++
	return out, nil
}

func (f *fakeBackupAPI) ListFrameworks(
	_ context.Context,
	_ *awsbackup.ListFrameworksInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.ListFrameworksOutput, error) {
	if f.listFrameworksCalls >= len(f.listFrameworks) {
		return &awsbackup.ListFrameworksOutput{}, nil
	}
	out := f.listFrameworks[f.listFrameworksCalls]
	f.listFrameworksCalls++
	return out, nil
}

func (f *fakeBackupAPI) DescribeFramework(
	_ context.Context,
	in *awsbackup.DescribeFrameworkInput,
	_ ...func(*awsbackup.Options),
) (*awsbackup.DescribeFrameworkOutput, error) {
	if f.describeFrameworks == nil {
		return &awsbackup.DescribeFrameworkOutput{}, nil
	}
	out, ok := f.describeFrameworks[aws.ToString(in.FrameworkName)]
	if !ok {
		return &awsbackup.DescribeFrameworkOutput{}, nil
	}
	return out, nil
}

var _ apiClient = (*fakeBackupAPI)(nil)
