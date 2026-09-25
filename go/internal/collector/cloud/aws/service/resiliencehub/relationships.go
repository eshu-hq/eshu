// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resiliencehub

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// appUsesPolicyRelationship records that an application is governed by a
// resiliency policy. AWS reports a policy ARN, which matches the resource_id the
// resiliency-policy node publishes. It returns nil when no policy is attached.
func appUsesPolicyRelationship(boundary aws.Boundary, app App) *aws.RelationshipObservation {
	policyARN := strings.TrimSpace(app.PolicyARN)
	if policyARN == "" {
		return nil
	}
	appID := appResourceID(app)
	if appID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipResilienceHubAppUsesPolicy,
		SourceResourceID: appID,
		SourceARN:        strings.TrimSpace(app.ARN),
		TargetResourceID: policyARN,
		TargetARN:        policyARN,
		TargetType:       aws.ResourceTypeResilienceHubResiliencyPolicy,
		SourceRecordID:   appID + "->" + aws.RelationshipResilienceHubAppUsesPolicy + ":" + policyARN,
	}
}

// appProtectsResourceRelationship records that an application protects a physical
// AWS resource. It is emitted only when the Resilience Hub-reported type maps to
// an Eshu resource family the owning scanner keys by ARN and the reported
// identifier is ARN-shaped, so the edge always joins the owning node. It returns
// nil otherwise, skipping the edge instead of dangling it.
func appProtectsResourceRelationship(
	boundary aws.Boundary,
	app App,
	resource ProtectedResource,
) *aws.RelationshipObservation {
	targetType := protectedResourceTargetType(resource.ResilienceHubType)
	targetARN := strings.TrimSpace(resource.ARN)
	if targetType == "" || !isARN(targetARN) {
		return nil
	}
	appID := appResourceID(app)
	if appID == "" {
		return nil
	}
	attributes := map[string]any{
		"resource_type": strings.TrimSpace(resource.ResilienceHubType),
	}
	if logical := strings.TrimSpace(resource.LogicalResourceID); logical != "" {
		attributes["logical_resource_id"] = logical
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipResilienceHubAppProtectsResource,
		SourceResourceID: appID,
		SourceARN:        strings.TrimSpace(app.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes:       attributes,
		SourceRecordID:   appID + "->" + aws.RelationshipResilienceHubAppProtectsResource + ":" + targetARN,
	}
}

// componentInAppRelationship records an application component's membership in its
// parent application, keyed by the application ARN the application node
// publishes. It returns nil when either endpoint identity is missing.
func componentInAppRelationship(
	boundary aws.Boundary,
	app App,
	component AppComponent,
) *aws.RelationshipObservation {
	appID := appResourceID(app)
	componentID := componentResourceID(appID, component)
	if appID == "" || componentID == "" {
		return nil
	}
	targetARN := ""
	if isARN(appID) {
		targetARN = appID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipResilienceHubComponentInApp,
		SourceResourceID: componentID,
		TargetResourceID: appID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeResilienceHubApp,
		SourceRecordID:   componentID + "->" + aws.RelationshipResilienceHubComponentInApp + ":" + appID,
	}
}

// inputSourceInAppRelationship records an input source's membership in its parent
// application, keyed by the application ARN the application node publishes. It
// returns nil when either endpoint identity is missing.
func inputSourceInAppRelationship(
	boundary aws.Boundary,
	app App,
	source InputSource,
) *aws.RelationshipObservation {
	appID := appResourceID(app)
	sourceID := inputSourceResourceID(appID, source)
	if appID == "" || sourceID == "" {
		return nil
	}
	sourceARN := ""
	if isARN(sourceID) {
		sourceARN = sourceID
	}
	targetARN := ""
	if isARN(appID) {
		targetARN = appID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipResilienceHubInputSourceInApp,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: appID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeResilienceHubApp,
		SourceRecordID:   sourceID + "->" + aws.RelationshipResilienceHubInputSourceInApp + ":" + appID,
	}
}

// assessmentForAppRelationship records that an assessment was run for an
// application, keyed by the application ARN the application node publishes. AWS
// reports the application ARN on the assessment summary directly. It returns nil
// when either endpoint identity is missing.
func assessmentForAppRelationship(
	boundary aws.Boundary,
	assessment Assessment,
) *aws.RelationshipObservation {
	assessmentID := assessmentResourceID(assessment)
	appARN := strings.TrimSpace(assessment.AppARN)
	if assessmentID == "" || appARN == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipResilienceHubAssessmentForApp,
		SourceResourceID: assessmentID,
		SourceARN:        strings.TrimSpace(assessment.ARN),
		TargetResourceID: appARN,
		TargetARN:        appARN,
		TargetType:       aws.ResourceTypeResilienceHubApp,
		SourceRecordID:   assessmentID + "->" + aws.RelationshipResilienceHubAssessmentForApp + ":" + appARN,
	}
}
