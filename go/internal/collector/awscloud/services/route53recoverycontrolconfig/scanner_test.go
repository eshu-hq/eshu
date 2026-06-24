package route53recoverycontrolconfig

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testClusterARN = "arn:aws:route53-recovery-control::123456789012:cluster/abcd1234"
	testPanelARN   = "arn:aws:route53-recovery-control::123456789012:controlpanel/efgh5678"
	testControlARN = "arn:aws:route53-recovery-control::123456789012:controlpanel/efgh5678/routingcontrol/ijkl9012"
	testRuleARN    = "arn:aws:route53-recovery-control::123456789012:controlpanel/efgh5678/safetyrule/mnop3456"
)

func fullSnapshot() Snapshot {
	return Snapshot{Clusters: []Cluster{{
		ARN:             testClusterARN,
		Name:            "prod-failover",
		Status:          "DEPLOYED",
		NetworkType:     "DUALSTACK",
		Owner:           "123456789012",
		EndpointRegions: []string{"us-east-1", "us-west-2"},
		Tags:            map[string]string{"Environment": "prod"},
		ControlPanels: []ControlPanel{{
			ARN:                 testPanelARN,
			ClusterARN:          testClusterARN,
			Name:                "main-panel",
			Status:              "DEPLOYED",
			DefaultControlPanel: true,
			RoutingControlCount: 1,
			Owner:               "123456789012",
			Tags:                map[string]string{"Team": "sre"},
			RoutingControls: []RoutingControl{{
				ARN:             testControlARN,
				ControlPanelARN: testPanelARN,
				Name:            "us-east-1-control",
				Status:          "DEPLOYED",
				Owner:           "123456789012",
			}},
			SafetyRules: []SafetyRule{{
				ARN:                  testRuleARN,
				ControlPanelARN:      testPanelARN,
				Name:                 "min-one-region",
				RuleKind:             "ASSERTION",
				Status:               "DEPLOYED",
				WaitPeriodMs:         5000,
				RuleConfigType:       "ATLEAST",
				RuleConfigThreshold:  1,
				RuleConfigInverted:   false,
				AssertedControlCount: 2,
			}},
		}},
	}}}
}

func TestScannerEmitsRecoveryControlMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(
		context.Background(), testBoundary(),
	)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeRoute53RecoveryControlConfigCluster)
	if got, want := cluster.Payload["resource_id"], testClusterARN; got != want {
		t.Fatalf("cluster resource_id = %#v, want %q", got, want)
	}
	if got, want := cluster.Payload["state"], "DEPLOYED"; got != want {
		t.Fatalf("cluster state = %#v, want %q", got, want)
	}
	clusterAttrs := attributesOf(t, cluster)
	assertAttribute(t, clusterAttrs, "network_type", "DUALSTACK")
	assertAttribute(t, clusterAttrs, "endpoint_regions", []string{"us-east-1", "us-west-2"})

	panel := resourceByType(t, envelopes, awscloud.ResourceTypeRoute53RecoveryControlConfigControlPanel)
	if got, want := panel.Payload["resource_id"], testPanelARN; got != want {
		t.Fatalf("panel resource_id = %#v, want %q", got, want)
	}
	panelAttrs := attributesOf(t, panel)
	assertAttribute(t, panelAttrs, "default_control_panel", true)
	assertAttribute(t, panelAttrs, "routing_control_count", int64(1))
	assertAttribute(t, panelAttrs, "cluster_arn", testClusterARN)

	control := resourceByType(t, envelopes, awscloud.ResourceTypeRoute53RecoveryControlConfigRoutingControl)
	if got, want := control.Payload["resource_id"], testControlARN; got != want {
		t.Fatalf("routing control resource_id = %#v, want %q", got, want)
	}

	rule := resourceByType(t, envelopes, awscloud.ResourceTypeRoute53RecoveryControlConfigSafetyRule)
	ruleAttrs := attributesOf(t, rule)
	assertAttribute(t, ruleAttrs, "rule_kind", "ASSERTION")
	assertAttribute(t, ruleAttrs, "rule_config_type", "ATLEAST")
	assertAttribute(t, ruleAttrs, "rule_config_threshold", int64(1))
	assertAttribute(t, ruleAttrs, "asserted_control_count", int64(2))

	// control panel -> cluster, keyed by the cluster ARN the cluster node publishes.
	panelInCluster := relationshipByType(
		t, envelopes, awscloud.RelationshipRoute53RecoveryControlConfigControlPanelInCluster,
	)
	assertEdgeTarget(t, panelInCluster, awscloud.ResourceTypeRoute53RecoveryControlConfigCluster, testClusterARN)
	if got, want := panelInCluster.Payload["source_resource_id"], testPanelARN; got != want {
		t.Fatalf("panel->cluster source_resource_id = %#v, want %q", got, want)
	}
	if got, want := panelInCluster.Payload["target_arn"], testClusterARN; got != want {
		t.Fatalf("panel->cluster target_arn = %#v, want %q", got, want)
	}

	// routing control -> control panel.
	controlInPanel := relationshipByType(
		t, envelopes, awscloud.RelationshipRoute53RecoveryControlConfigRoutingControlInControlPanel,
	)
	assertEdgeTarget(
		t, controlInPanel, awscloud.ResourceTypeRoute53RecoveryControlConfigControlPanel, testPanelARN,
	)
	if got, want := controlInPanel.Payload["source_resource_id"], testControlARN; got != want {
		t.Fatalf("control->panel source_resource_id = %#v, want %q", got, want)
	}

	// safety rule -> control panel.
	ruleInPanel := relationshipByType(
		t, envelopes, awscloud.RelationshipRoute53RecoveryControlConfigSafetyRuleInControlPanel,
	)
	assertEdgeTarget(t, ruleInPanel, awscloud.ResourceTypeRoute53RecoveryControlConfigControlPanel, testPanelARN)
	if got, want := ruleInPanel.Payload["source_resource_id"], testRuleARN; got != want {
		t.Fatalf("rule->panel source_resource_id = %#v, want %q", got, want)
	}

	// No routing control state leakage in any resource payload.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"routing_control_state", "control_state", "state_on", "endpoint_url",
			"endpoint_urls", "cluster_endpoints",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; recovery-control scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerEmitsGatingRuleCounts(t *testing.T) {
	snapshot := Snapshot{Clusters: []Cluster{{
		ARN:  testClusterARN,
		Name: "prod",
		ControlPanels: []ControlPanel{{
			ARN:        testPanelARN,
			ClusterARN: testClusterARN,
			Name:       "panel",
			SafetyRules: []SafetyRule{{
				ARN:                testRuleARN,
				ControlPanelARN:    testPanelARN,
				Name:               "gate",
				RuleKind:           "GATING",
				RuleConfigType:     "AND",
				GatingControlCount: 1,
				TargetControlCount: 3,
			}},
		}},
	}}}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rule := resourceByType(t, envelopes, awscloud.ResourceTypeRoute53RecoveryControlConfigSafetyRule)
	ruleAttrs := attributesOf(t, rule)
	assertAttribute(t, ruleAttrs, "rule_kind", "GATING")
	assertAttribute(t, ruleAttrs, "gating_control_count", int64(1))
	assertAttribute(t, ruleAttrs, "target_control_count", int64(3))
	if _, exists := ruleAttrs["asserted_control_count"]; exists {
		t.Fatalf("gating rule must not carry asserted_control_count")
	}
}

func TestScannerOmitsControlPanelEdgeWhenClusterARNAbsent(t *testing.T) {
	snapshot := Snapshot{Clusters: []Cluster{{
		ARN:  testClusterARN,
		Name: "prod",
		ControlPanels: []ControlPanel{{
			ARN:  testPanelARN,
			Name: "orphan-panel",
			// No ClusterARN: no membership edge.
		}},
	}}}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got ==
			awscloud.RelationshipRoute53RecoveryControlConfigControlPanelInCluster {
			t.Fatalf("unexpected control-panel-in-cluster edge with no cluster ARN")
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	panel := ControlPanel{ARN: testPanelARN, ClusterARN: testClusterARN, Name: "panel"}
	control := RoutingControl{ARN: testControlARN, ControlPanelARN: testPanelARN, Name: "control"}
	rule := SafetyRule{ARN: testRuleARN, ControlPanelARN: testPanelARN, Name: "rule", RuleKind: "ASSERTION"}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		controlPanelInClusterRelationship(boundary, panel),
		routingControlInControlPanelRelationship(boundary, control),
		safetyRuleInControlPanelRelationship(boundary, rule),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerGovCloudClusterUsesReportedARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	govClusterARN := "arn:aws-us-gov:route53-recovery-control::123456789012:cluster/gov1234"
	govPanelARN := "arn:aws-us-gov:route53-recovery-control::123456789012:controlpanel/gov5678"
	snapshot := Snapshot{Clusters: []Cluster{{
		ARN:  govClusterARN,
		Name: "gov-failover",
		ControlPanels: []ControlPanel{{
			ARN:        govPanelARN,
			ClusterARN: govClusterARN,
			Name:       "gov-panel",
		}},
	}}}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	panelInCluster := relationshipByType(
		t, envelopes, awscloud.RelationshipRoute53RecoveryControlConfigControlPanelInCluster,
	)
	if got := panelInCluster.Payload["target_resource_id"]; got != govClusterARN {
		t.Fatalf("GovCloud panel->cluster target_resource_id = %#v, want %q", got, govClusterARN)
	}
	if got := panelInCluster.Payload["target_arn"]; got != govClusterARN {
		t.Fatalf("GovCloud panel->cluster target_arn = %#v, want %q", got, govClusterARN)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Clusters: []Cluster{{ARN: testClusterARN, Name: "prod"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:    testBoundary(),
			WarningKind: awscloud.WarningThrottleSustained,
			ErrorClass:  "throttled",
			Message: "Route 53 ARC ListSafetyRules throttled after SDK retries; " +
				"safety rule metadata omitted for this scan",
			SourceRecordID: "route53recoverycontrolconfig_safety_rules_throttled",
		}},
	}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-west-2",
		ServiceKind:         awscloud.ServiceRoute53RecoveryControlConfig,
		ScopeID:             "aws:123456789012:us-west-2",
		GenerationID:        "aws:123456789012:us-west-2:route53recoverycontrolconfig:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
