package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsresiliencehub "github.com/aws/aws-sdk-go-v2/service/resiliencehub"
	awsresiliencehubtypes "github.com/aws/aws-sdk-go-v2/service/resiliencehub/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	resiliencehubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/resiliencehub"
)

// listApps pages every application, then enriches each with its policy
// reference (via DescribeApp), input sources, components, protected physical
// resources, and assessments. A missing published application version yields a
// warning and an app carrying only its summary metadata rather than a hard
// failure, so one unprepared application never blocks the whole scan.
func (c *Client) listApps(ctx context.Context) ([]resiliencehubservice.App, []awscloud.WarningObservation, error) {
	summaries, err := c.listAppSummaries(ctx)
	if err != nil {
		return nil, nil, err
	}
	var (
		apps     []resiliencehubservice.App
		warnings []awscloud.WarningObservation
	)
	for _, summary := range summaries {
		app, appWarnings, appErr := c.enrichApp(ctx, summary)
		if appErr != nil {
			return nil, nil, appErr
		}
		apps = append(apps, app)
		warnings = append(warnings, appWarnings...)
	}
	return apps, warnings, nil
}

func (c *Client) listAppSummaries(ctx context.Context) ([]awsresiliencehubtypes.AppSummary, error) {
	var summaries []awsresiliencehubtypes.AppSummary
	var nextToken *string
	for {
		var page *awsresiliencehub.ListAppsOutput
		err := c.recordAPICall(ctx, "ListApps", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListApps(callCtx, &awsresiliencehub.ListAppsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return summaries, nil
		}
		summaries = append(summaries, page.AppSummaries...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return summaries, nil
		}
	}
}

// enrichApp turns one app summary into the full scanner-owned app, gathering the
// policy ARN and version-scoped metadata. The published version may not exist
// for an app that has never been published; that yields a warning, not an error.
func (c *Client) enrichApp(
	ctx context.Context,
	summary awsresiliencehubtypes.AppSummary,
) (resiliencehubservice.App, []awscloud.WarningObservation, error) {
	appARN := strings.TrimSpace(aws.ToString(summary.AppArn))
	app := resiliencehubservice.App{
		ARN:                appARN,
		Name:               strings.TrimSpace(aws.ToString(summary.Name)),
		Description:        strings.TrimSpace(aws.ToString(summary.Description)),
		Status:             strings.TrimSpace(string(summary.Status)),
		ComplianceStatus:   strings.TrimSpace(string(summary.ComplianceStatus)),
		DriftStatus:        strings.TrimSpace(string(summary.DriftStatus)),
		AssessmentSchedule: strings.TrimSpace(string(summary.AssessmentSchedule)),
		AWSApplicationARN:  strings.TrimSpace(aws.ToString(summary.AwsApplicationArn)),
		ResiliencyScore:    summary.ResiliencyScore,
		RPOInSecs:          summary.RpoInSecs,
		RTOInSecs:          summary.RtoInSecs,
		CreationTime:       aws.ToTime(summary.CreationTime),
	}
	if appARN == "" {
		return app, nil, nil
	}

	policyARN, tags, err := c.describeApp(ctx, appARN)
	if err != nil {
		return resiliencehubservice.App{}, nil, err
	}
	app.PolicyARN = policyARN
	app.Tags = tags

	assessments, err := c.listAppAssessments(ctx, appARN)
	if err != nil {
		return resiliencehubservice.App{}, nil, err
	}
	app.Assessments = assessments

	var warnings []awscloud.WarningObservation
	versioned, versionWarning, err := c.listVersionedMetadata(ctx, appARN)
	if err != nil {
		return resiliencehubservice.App{}, nil, err
	}
	app.InputSources = versioned.inputSources
	app.Components = versioned.components
	app.ProtectedResources = versioned.protectedResources
	if versionWarning != nil {
		warnings = append(warnings, *versionWarning)
	}
	return app, warnings, nil
}

func (c *Client) describeApp(ctx context.Context, appARN string) (policyARN string, tags map[string]string, err error) {
	var output *awsresiliencehub.DescribeAppOutput
	err = c.recordAPICall(ctx, "DescribeApp", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeApp(callCtx, &awsresiliencehub.DescribeAppInput{
			AppArn: aws.String(appARN),
		})
		return callErr
	})
	if err != nil || output == nil || output.App == nil {
		return "", nil, err
	}
	return strings.TrimSpace(aws.ToString(output.App.PolicyArn)), cloneTags(output.App.Tags), nil
}

func (c *Client) listAppAssessments(ctx context.Context, appARN string) ([]resiliencehubservice.Assessment, error) {
	var assessments []resiliencehubservice.Assessment
	var nextToken *string
	for {
		var page *awsresiliencehub.ListAppAssessmentsOutput
		err := c.recordAPICall(ctx, "ListAppAssessments", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListAppAssessments(callCtx, &awsresiliencehub.ListAppAssessmentsInput{
				AppArn:    aws.String(appARN),
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
		for _, summary := range page.AssessmentSummaries {
			assessments = append(assessments, mapAssessment(summary))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return assessments, nil
		}
	}
}

func mapAssessment(summary awsresiliencehubtypes.AppAssessmentSummary) resiliencehubservice.Assessment {
	return resiliencehubservice.Assessment{
		ARN:              strings.TrimSpace(aws.ToString(summary.AssessmentArn)),
		AppARN:           strings.TrimSpace(aws.ToString(summary.AppArn)),
		Name:             strings.TrimSpace(aws.ToString(summary.AssessmentName)),
		Status:           strings.TrimSpace(string(summary.AssessmentStatus)),
		ComplianceStatus: strings.TrimSpace(string(summary.ComplianceStatus)),
		DriftStatus:      strings.TrimSpace(string(summary.DriftStatus)),
		Invoker:          strings.TrimSpace(string(summary.Invoker)),
		AppVersion:       strings.TrimSpace(aws.ToString(summary.AppVersion)),
		ResiliencyScore:  summary.ResiliencyScore,
		StartTime:        aws.ToTime(summary.StartTime),
		EndTime:          aws.ToTime(summary.EndTime),
	}
}

// versionedMetadata bundles the published-version reads for one application.
type versionedMetadata struct {
	inputSources       []resiliencehubservice.InputSource
	components         []resiliencehubservice.AppComponent
	protectedResources []resiliencehubservice.ProtectedResource
}

// listVersionedMetadata reads the published-version input sources, components,
// and physical resources for one app. When the published version does not exist
// (ResourceNotFoundException) it returns empty metadata and a warning so the app
// is still recorded with its summary identity instead of failing the scan.
func (c *Client) listVersionedMetadata(
	ctx context.Context,
	appARN string,
) (versionedMetadata, *awscloud.WarningObservation, error) {
	inputSources, err := c.listInputSources(ctx, appARN)
	if err != nil {
		if isResourceNotFound(err) {
			return versionedMetadata{}, c.versionMissingWarning(appARN), nil
		}
		return versionedMetadata{}, nil, err
	}
	components, err := c.listComponents(ctx, appARN)
	if err != nil {
		if isResourceNotFound(err) {
			return versionedMetadata{}, c.versionMissingWarning(appARN), nil
		}
		return versionedMetadata{}, nil, err
	}
	resources, err := c.listProtectedResources(ctx, appARN)
	if err != nil {
		if isResourceNotFound(err) {
			return versionedMetadata{}, c.versionMissingWarning(appARN), nil
		}
		return versionedMetadata{}, nil, err
	}
	return versionedMetadata{
		inputSources:       inputSources,
		components:         components,
		protectedResources: resources,
	}, nil, nil
}

func (c *Client) versionMissingWarning(appARN string) *awscloud.WarningObservation {
	return &awscloud.WarningObservation{
		Boundary:    c.boundary,
		WarningKind: awscloud.WarningResilienceHubAppVersionMissing,
		ErrorClass:  "resource_not_found",
		Message: fmt.Sprintf(
			"Resilience Hub application %q has no %q version; input sources, components, and protected resources omitted for this scan",
			appARN, publishedAppVersion,
		),
		SourceRecordID: "resiliencehub_app_version_missing:" + appARN,
	}
}
