// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsauditmanager "github.com/aws/aws-sdk-go-v2/service/auditmanager"
	awsauditmanagertypes "github.com/aws/aws-sdk-go-v2/service/auditmanager/types"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	auditmanagerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/auditmanager"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Audit Manager API the adapter
// calls. It is deliberately limited to account-status, assessment, framework,
// control, settings, and resource-tag reads. It exposes no GetEvidence,
// GetEvidenceFolder, GetChangeLogs, GetDelegations, GetAssessmentReportUrl,
// GetControl (control narrative), or any Create/Update/Delete/Register/Batch
// mutation, so the adapter cannot read collected evidence, evidence finder
// records, change logs, delegation comments, control narratives, or report URLs
// and cannot mutate Audit Manager state. The exclusion_test reflects over this
// interface to enforce that contract at build time.
type apiClient interface {
	GetAccountStatus(
		context.Context,
		*awsauditmanager.GetAccountStatusInput,
		...func(*awsauditmanager.Options),
	) (*awsauditmanager.GetAccountStatusOutput, error)
	ListAssessments(
		context.Context,
		*awsauditmanager.ListAssessmentsInput,
		...func(*awsauditmanager.Options),
	) (*awsauditmanager.ListAssessmentsOutput, error)
	GetAssessment(
		context.Context,
		*awsauditmanager.GetAssessmentInput,
		...func(*awsauditmanager.Options),
	) (*awsauditmanager.GetAssessmentOutput, error)
	ListAssessmentFrameworks(
		context.Context,
		*awsauditmanager.ListAssessmentFrameworksInput,
		...func(*awsauditmanager.Options),
	) (*awsauditmanager.ListAssessmentFrameworksOutput, error)
	ListControls(
		context.Context,
		*awsauditmanager.ListControlsInput,
		...func(*awsauditmanager.Options),
	) (*awsauditmanager.ListControlsOutput, error)
	GetSettings(
		context.Context,
		*awsauditmanager.GetSettingsInput,
		...func(*awsauditmanager.Options),
	) (*awsauditmanager.GetSettingsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsauditmanager.ListTagsForResourceInput,
		...func(*awsauditmanager.Options),
	) (*awsauditmanager.ListTagsForResourceOutput, error)
}

// frameworkTypes are the framework-type filters ListAssessmentFrameworks
// requires; the adapter iterates both to enumerate standard and custom
// frameworks to exhaustion.
var frameworkTypes = []awsauditmanagertypes.FrameworkType{
	awsauditmanagertypes.FrameworkTypeStandard,
	awsauditmanagertypes.FrameworkTypeCustom,
}

// controlTypes are the control-type filters ListControls requires; the adapter
// iterates all three to enumerate standard, custom, and core controls.
var controlTypes = []awsauditmanagertypes.ControlType{
	awsauditmanagertypes.ControlTypeStandard,
	awsauditmanagertypes.ControlTypeCustom,
	awsauditmanagertypes.ControlTypeCore,
}

// Client adapts AWS SDK Audit Manager control-plane calls into scanner-owned
// metadata. It never reads collected evidence, evidence finder records, change
// logs, delegation comments, control narratives, or assessment report URLs, and
// never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	accountID   string
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Audit Manager SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsauditmanager.NewFromConfig(config),
		boundary:    boundary,
		accountID:   strings.TrimSpace(boundary.AccountID),
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Audit Manager assessment, framework, and control metadata plus
// the account-level KMS key visible to the configured AWS credentials. An
// account that has not enabled Audit Manager yields an empty result with a
// warning rather than a scan failure.
func (c *Client) Snapshot(ctx context.Context) (auditmanagerservice.Snapshot, error) {
	registered, statusErr := c.accountRegistered(ctx)
	if statusErr != nil {
		if isNotRegistered(statusErr) {
			return auditmanagerservice.Snapshot{Warnings: []awscloud.WarningObservation{c.notRegisteredWarning(statusErr)}}, nil
		}
		return auditmanagerservice.Snapshot{}, statusErr
	}
	if !registered {
		return auditmanagerservice.Snapshot{Warnings: []awscloud.WarningObservation{c.notRegisteredWarning(nil)}}, nil
	}

	assessments, err := c.listAssessments(ctx)
	if err != nil {
		return auditmanagerservice.Snapshot{}, err
	}
	frameworks, err := c.listFrameworks(ctx)
	if err != nil {
		return auditmanagerservice.Snapshot{}, err
	}
	controls, err := c.listControls(ctx)
	if err != nil {
		return auditmanagerservice.Snapshot{}, err
	}
	kmsKeyARN, err := c.settingsKMSKey(ctx)
	if err != nil {
		return auditmanagerservice.Snapshot{}, err
	}
	return auditmanagerservice.Snapshot{
		Assessments: assessments,
		Frameworks:  frameworks,
		Controls:    controls,
		KMSKeyARN:   kmsKeyARN,
	}, nil
}

// accountRegistered reports whether Audit Manager is active for the account. A
// PENDING_ACTIVATION or INACTIVE status means there are no resources to read.
func (c *Client) accountRegistered(ctx context.Context) (bool, error) {
	var output *awsauditmanager.GetAccountStatusOutput
	err := c.recordAPICall(ctx, "GetAccountStatus", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetAccountStatus(callCtx, &awsauditmanager.GetAccountStatusInput{})
		return callErr
	})
	if err != nil {
		return false, err
	}
	if output == nil {
		return false, nil
	}
	return output.Status == awsauditmanagertypes.AccountStatusActive, nil
}

func (c *Client) listAssessments(ctx context.Context) ([]auditmanagerservice.Assessment, error) {
	var assessments []auditmanagerservice.Assessment
	var nextToken *string
	for {
		var page *awsauditmanager.ListAssessmentsOutput
		err := c.recordAPICall(ctx, "ListAssessments", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListAssessments(callCtx, &awsauditmanager.ListAssessmentsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return assessments, nil
		}
		for _, item := range page.AssessmentMetadata {
			detail, err := c.getAssessment(ctx, aws.ToString(item.Id))
			if err != nil {
				return nil, err
			}
			assessments = append(assessments, detail)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return assessments, nil
		}
	}
}

func (c *Client) getAssessment(ctx context.Context, assessmentID string) (auditmanagerservice.Assessment, error) {
	assessmentID = strings.TrimSpace(assessmentID)
	if assessmentID == "" {
		return auditmanagerservice.Assessment{}, nil
	}
	var output *awsauditmanager.GetAssessmentOutput
	err := c.recordAPICall(ctx, "GetAssessment", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetAssessment(callCtx, &awsauditmanager.GetAssessmentInput{
			AssessmentId: aws.String(assessmentID),
		})
		return callErr
	})
	if err != nil {
		return auditmanagerservice.Assessment{}, err
	}
	if output == nil {
		return auditmanagerservice.Assessment{}, nil
	}
	return mapAssessment(output.Assessment), nil
}

func (c *Client) listFrameworks(ctx context.Context) ([]auditmanagerservice.Framework, error) {
	var frameworks []auditmanagerservice.Framework
	for _, frameworkType := range frameworkTypes {
		var nextToken *string
		for {
			var page *awsauditmanager.ListAssessmentFrameworksOutput
			err := c.recordAPICall(ctx, "ListAssessmentFrameworks", func(callCtx context.Context) error {
				var callErr error
				page, callErr = c.client.ListAssessmentFrameworks(callCtx, &awsauditmanager.ListAssessmentFrameworksInput{
					FrameworkType: frameworkType,
					NextToken:     nextToken,
				})
				return callErr
			})
			if err != nil {
				return nil, err
			}
			if page == nil {
				break
			}
			for _, item := range page.FrameworkMetadataList {
				frameworks = append(frameworks, mapFramework(item))
			}
			nextToken = page.NextToken
			if aws.ToString(nextToken) == "" {
				break
			}
		}
	}
	return frameworks, nil
}

func (c *Client) listControls(ctx context.Context) ([]auditmanagerservice.Control, error) {
	var controls []auditmanagerservice.Control
	for _, controlType := range controlTypes {
		var nextToken *string
		for {
			var page *awsauditmanager.ListControlsOutput
			err := c.recordAPICall(ctx, "ListControls", func(callCtx context.Context) error {
				var callErr error
				page, callErr = c.client.ListControls(callCtx, &awsauditmanager.ListControlsInput{
					ControlType: controlType,
					NextToken:   nextToken,
				})
				return callErr
			})
			if err != nil {
				return nil, err
			}
			if page == nil {
				break
			}
			for _, item := range page.ControlMetadataList {
				controls = append(controls, mapControl(item, string(controlType)))
			}
			nextToken = page.NextToken
			if aws.ToString(nextToken) == "" {
				break
			}
		}
	}
	return controls, nil
}

// settingsKMSKey returns the account-level customer managed KMS key Audit Manager
// uses to encrypt assessment evidence and reports, or "" when Audit Manager uses
// an AWS-owned key. A denied or missing settings read degrades to no KMS edge
// rather than failing the scan.
func (c *Client) settingsKMSKey(ctx context.Context) (string, error) {
	var output *awsauditmanager.GetSettingsOutput
	err := c.recordAPICall(ctx, "GetSettings", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetSettings(callCtx, &awsauditmanager.GetSettingsInput{
			Attribute: awsauditmanagertypes.SettingAttributeAll,
		})
		return callErr
	})
	if err != nil {
		if isNotRegistered(err) {
			return "", nil
		}
		return "", err
	}
	if output == nil || output.Settings == nil {
		return "", nil
	}
	return strings.TrimSpace(aws.ToString(output.Settings.KmsKey)), nil
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

func (c *Client) notRegisteredWarning(err error) awscloud.WarningObservation {
	class := "not_registered"
	if err != nil {
		class = errorClass(err)
	}
	return awscloud.WarningObservation{
		Boundary:       c.boundary,
		WarningKind:    "auditmanager_not_registered",
		ErrorClass:     class,
		Message:        "account has not enabled Amazon Audit Manager; no Audit Manager resources observed",
		SourceRecordID: "auditmanager_not_registered:" + c.accountID,
	}
}

var _ auditmanagerservice.Client = (*Client)(nil)

var _ apiClient = (*awsauditmanager.Client)(nil)
