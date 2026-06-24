package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbackup "github.com/aws/aws-sdk-go-v2/service/backup"
	awsbackuptypes "github.com/aws/aws-sdk-go-v2/service/backup/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	backupservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/backup"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the narrowed AWS Backup API surface this adapter calls. The
// type intentionally lists only the metadata reads the scanner needs.
// Mutation APIs, StartBackupJob/StartRestoreJob/StartCopyJob,
// PutBackupVaultAccessPolicy, GetBackupVaultAccessPolicy, and
// GetRecoveryPointRestoreMetadata are NOT included. The acceptance test for
// this scanner asserts the scanner-level Client interface exposes no
// mutation methods; this adapter goes further by also keeping forbidden
// APIs out of the SDK surface used here.
type apiClient interface {
	ListBackupVaults(ctx context.Context, in *awsbackup.ListBackupVaultsInput, optFns ...func(*awsbackup.Options)) (*awsbackup.ListBackupVaultsOutput, error)
	DescribeBackupVault(ctx context.Context, in *awsbackup.DescribeBackupVaultInput, optFns ...func(*awsbackup.Options)) (*awsbackup.DescribeBackupVaultOutput, error)
	ListBackupPlans(ctx context.Context, in *awsbackup.ListBackupPlansInput, optFns ...func(*awsbackup.Options)) (*awsbackup.ListBackupPlansOutput, error)
	GetBackupPlan(ctx context.Context, in *awsbackup.GetBackupPlanInput, optFns ...func(*awsbackup.Options)) (*awsbackup.GetBackupPlanOutput, error)
	ListBackupSelections(ctx context.Context, in *awsbackup.ListBackupSelectionsInput, optFns ...func(*awsbackup.Options)) (*awsbackup.ListBackupSelectionsOutput, error)
	GetBackupSelection(ctx context.Context, in *awsbackup.GetBackupSelectionInput, optFns ...func(*awsbackup.Options)) (*awsbackup.GetBackupSelectionOutput, error)
	ListRecoveryPointsByBackupVault(ctx context.Context, in *awsbackup.ListRecoveryPointsByBackupVaultInput, optFns ...func(*awsbackup.Options)) (*awsbackup.ListRecoveryPointsByBackupVaultOutput, error)
	ListReportPlans(ctx context.Context, in *awsbackup.ListReportPlansInput, optFns ...func(*awsbackup.Options)) (*awsbackup.ListReportPlansOutput, error)
	ListRestoreTestingPlans(ctx context.Context, in *awsbackup.ListRestoreTestingPlansInput, optFns ...func(*awsbackup.Options)) (*awsbackup.ListRestoreTestingPlansOutput, error)
	ListFrameworks(ctx context.Context, in *awsbackup.ListFrameworksInput, optFns ...func(*awsbackup.Options)) (*awsbackup.ListFrameworksOutput, error)
	DescribeFramework(ctx context.Context, in *awsbackup.DescribeFrameworkInput, optFns ...func(*awsbackup.Options)) (*awsbackup.DescribeFrameworkOutput, error)
}

// Client adapts AWS SDK Backup pagination into scanner-owned metadata.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an AWS Backup SDK adapter for one claimed boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsbackup.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListBackupVaults returns AWS Backup vault metadata visible to the
// configured credentials.
func (c *Client) ListBackupVaults(ctx context.Context) ([]backupservice.Vault, error) {
	paginator := awsbackup.NewListBackupVaultsPaginator(c.client, &awsbackup.ListBackupVaultsInput{})
	var vaults []backupservice.Vault
	for paginator.HasMorePages() {
		var page *awsbackup.ListBackupVaultsOutput
		err := c.recordAPICall(ctx, "ListBackupVaults", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, item := range page.BackupVaultList {
			vault := mapVaultListMember(item)
			described, err := c.describeBackupVault(ctx, aws.ToString(item.BackupVaultName))
			if err != nil {
				return nil, err
			}
			mergeDescribedVault(&vault, described)
			vaults = append(vaults, vault)
		}
	}
	return vaults, nil
}

func (c *Client) describeBackupVault(
	ctx context.Context,
	name string,
) (*awsbackup.DescribeBackupVaultOutput, error) {
	if strings.TrimSpace(name) == "" {
		return &awsbackup.DescribeBackupVaultOutput{}, nil
	}
	var output *awsbackup.DescribeBackupVaultOutput
	err := c.recordAPICall(ctx, "DescribeBackupVault", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeBackupVault(callCtx, &awsbackup.DescribeBackupVaultInput{
			BackupVaultName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awsbackup.DescribeBackupVaultOutput{}, nil
	}
	return output, nil
}

// ListBackupPlans returns AWS Backup plan metadata. The function fetches each
// plan body to surface rule-level schedule and target-vault metadata; rule
// recovery point tag values, lifecycle settings, and copy actions stay out of
// the scanner contract.
func (c *Client) ListBackupPlans(ctx context.Context) ([]backupservice.Plan, error) {
	paginator := awsbackup.NewListBackupPlansPaginator(c.client, &awsbackup.ListBackupPlansInput{})
	var plans []backupservice.Plan
	for paginator.HasMorePages() {
		var page *awsbackup.ListBackupPlansOutput
		err := c.recordAPICall(ctx, "ListBackupPlans", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, item := range page.BackupPlansList {
			plan := mapPlanListMember(item)
			rules, err := c.getBackupPlanRules(ctx, aws.ToString(item.BackupPlanId))
			if err != nil {
				return nil, err
			}
			plan.Rules = rules
			plans = append(plans, plan)
		}
	}
	return plans, nil
}

func (c *Client) getBackupPlanRules(
	ctx context.Context,
	planID string,
) ([]backupservice.PlanRule, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return nil, nil
	}
	var output *awsbackup.GetBackupPlanOutput
	err := c.recordAPICall(ctx, "GetBackupPlan", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBackupPlan(callCtx, &awsbackup.GetBackupPlanInput{
			BackupPlanId: aws.String(planID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.BackupPlan == nil {
		return nil, nil
	}
	return mapPlanRules(output.BackupPlan.Rules), nil
}

// ListBackupSelections returns the selections owned by one plan.
func (c *Client) ListBackupSelections(
	ctx context.Context,
	planID string,
) ([]backupservice.Selection, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return nil, nil
	}
	paginator := awsbackup.NewListBackupSelectionsPaginator(c.client, &awsbackup.ListBackupSelectionsInput{
		BackupPlanId: aws.String(planID),
	})
	var selections []backupservice.Selection
	for paginator.HasMorePages() {
		var page *awsbackup.ListBackupSelectionsOutput
		err := c.recordAPICall(ctx, "ListBackupSelections", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, item := range page.BackupSelectionsList {
			selection := mapSelectionListMember(item)
			body, err := c.getBackupSelection(ctx, planID, aws.ToString(item.SelectionId))
			if err != nil {
				return nil, err
			}
			mergeDescribedSelection(&selection, body)
			selections = append(selections, selection)
		}
	}
	return selections, nil
}

func (c *Client) getBackupSelection(
	ctx context.Context,
	planID string,
	selectionID string,
) (*awsbackup.GetBackupSelectionOutput, error) {
	selectionID = strings.TrimSpace(selectionID)
	if selectionID == "" {
		return &awsbackup.GetBackupSelectionOutput{}, nil
	}
	var output *awsbackup.GetBackupSelectionOutput
	err := c.recordAPICall(ctx, "GetBackupSelection", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBackupSelection(callCtx, &awsbackup.GetBackupSelectionInput{
			BackupPlanId: aws.String(planID),
			SelectionId:  aws.String(selectionID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awsbackup.GetBackupSelectionOutput{}, nil
	}
	return output, nil
}

// ListRecoveryPoints returns recovery point metadata for one vault. The
// adapter never reads recovery point contents and never calls
// GetRecoveryPointRestoreMetadata because the restore metadata map can echo
// source-resource configuration values.
func (c *Client) ListRecoveryPoints(
	ctx context.Context,
	vaultName string,
) ([]backupservice.RecoveryPoint, error) {
	vaultName = strings.TrimSpace(vaultName)
	if vaultName == "" {
		return nil, nil
	}
	paginator := awsbackup.NewListRecoveryPointsByBackupVaultPaginator(
		c.client,
		&awsbackup.ListRecoveryPointsByBackupVaultInput{BackupVaultName: aws.String(vaultName)},
	)
	var recoveryPoints []backupservice.RecoveryPoint
	for paginator.HasMorePages() {
		var page *awsbackup.ListRecoveryPointsByBackupVaultOutput
		err := c.recordAPICall(ctx, "ListRecoveryPointsByBackupVault", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, item := range page.RecoveryPoints {
			recoveryPoints = append(recoveryPoints, mapRecoveryPoint(item))
		}
	}
	return recoveryPoints, nil
}

// ListReportPlans returns AWS Backup report plan metadata.
func (c *Client) ListReportPlans(ctx context.Context) ([]backupservice.ReportPlan, error) {
	paginator := awsbackup.NewListReportPlansPaginator(c.client, &awsbackup.ListReportPlansInput{})
	var plans []backupservice.ReportPlan
	for paginator.HasMorePages() {
		var page *awsbackup.ListReportPlansOutput
		err := c.recordAPICall(ctx, "ListReportPlans", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, item := range page.ReportPlans {
			plans = append(plans, mapReportPlan(item))
		}
	}
	return plans, nil
}

// ListRestoreTestingPlans returns AWS Backup restore testing plan metadata.
func (c *Client) ListRestoreTestingPlans(ctx context.Context) ([]backupservice.RestoreTestingPlan, error) {
	paginator := awsbackup.NewListRestoreTestingPlansPaginator(c.client, &awsbackup.ListRestoreTestingPlansInput{})
	var plans []backupservice.RestoreTestingPlan
	for paginator.HasMorePages() {
		var page *awsbackup.ListRestoreTestingPlansOutput
		err := c.recordAPICall(ctx, "ListRestoreTestingPlans", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, item := range page.RestoreTestingPlans {
			plans = append(plans, mapRestoreTestingPlan(item))
		}
	}
	return plans, nil
}

// ListFrameworks returns AWS Backup framework metadata, including control
// summaries. Control input parameter VALUES are never persisted.
func (c *Client) ListFrameworks(ctx context.Context) ([]backupservice.Framework, error) {
	paginator := awsbackup.NewListFrameworksPaginator(c.client, &awsbackup.ListFrameworksInput{})
	var frameworks []backupservice.Framework
	for paginator.HasMorePages() {
		var page *awsbackup.ListFrameworksOutput
		err := c.recordAPICall(ctx, "ListFrameworks", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		for _, item := range page.Frameworks {
			framework := mapFrameworkListItem(item)
			controls, err := c.describeFrameworkControls(ctx, aws.ToString(item.FrameworkName))
			if err != nil {
				return nil, err
			}
			framework.Controls = controls
			frameworks = append(frameworks, framework)
		}
	}
	return frameworks, nil
}

func (c *Client) describeFrameworkControls(
	ctx context.Context,
	name string,
) ([]backupservice.FrameworkControl, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	var output *awsbackup.DescribeFrameworkOutput
	err := c.recordAPICall(ctx, "DescribeFramework", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeFramework(callCtx, &awsbackup.DescribeFrameworkInput{
			FrameworkName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return mapFrameworkControls(output.FrameworkControls), nil
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

var (
	_ backupservice.Client = (*Client)(nil)
	_ apiClient            = (*awsbackup.Client)(nil)
	// Static type pin so the awsbackuptypes import is exercised by the
	// mapping helpers compiled in mapping.go.
	_ awsbackuptypes.VaultType = ""
)
