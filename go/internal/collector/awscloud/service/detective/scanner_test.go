// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detective

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testGraphARN    = "arn:aws:detective:us-east-1:123456789012:graph:abc123def456abc123def456abc12345"
	testMemberAcct  = "111122223333"
	testAdminAcct   = "123456789012"
	testDetectorID  = "12abc34d567e8fa901bc2d34eexample"
	testMemberEmail = "security@example.com"
)

func TestScannerEmitsDetectiveGraphAndMemberAccountEdge(t *testing.T) {
	client := fakeClient{graphs: []Graph{{
		ARN:       testGraphARN,
		CreatedAt: "2026-05-27T12:00:00Z",
	}}, members: map[string][]MemberAccount{
		testGraphARN: {{
			AccountID:          testMemberAcct,
			AdministratorID:    testAdminAcct,
			GraphARN:           testGraphARN,
			Status:             "ENABLED",
			InvitationType:     "ORGANIZATION",
			InvitedAt:          "2026-05-27T12:01:00Z",
			UpdatedAt:          "2026-05-27T12:02:00Z",
			DatasourcePackages: []string{"DETECTIVE_CORE"},
		}},
	}, tags: map[string]map[string]string{
		testGraphARN: {"Environment": "prod"},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	graph := resourceByType(t, envelopes, awscloud.ResourceTypeDetectiveGraph)
	if got, want := graph.Payload["resource_id"], testGraphARN; got != want {
		t.Fatalf("graph resource_id = %#v, want %q", got, want)
	}
	if got, want := graph.Payload["arn"], testGraphARN; got != want {
		t.Fatalf("graph arn = %#v, want %q", got, want)
	}
	graphAttrs := attributesOf(t, graph)
	if got, want := graphAttrs["created_at"], "2026-05-27T12:00:00Z"; got != want {
		t.Fatalf("graph created_at = %#v, want %q", got, want)
	}
	if got, want := graphAttrs["member_account_count"], 1; got != want {
		t.Fatalf("graph member_account_count = %#v, want %d", got, want)
	}
	if got := graphAttrs["sources_guardduty_data"]; got != true {
		t.Fatalf("graph sources_guardduty_data = %#v, want true (DETECTIVE_CORE present)", got)
	}

	member := resourceByType(t, envelopes, awscloud.ResourceTypeDetectiveMemberAccount)
	wantMemberID := testGraphARN + "/member/" + testMemberAcct
	if got := member.Payload["resource_id"]; got != wantMemberID {
		t.Fatalf("member resource_id = %#v, want %q", got, wantMemberID)
	}
	memberAttrs := attributesOf(t, member)
	if got, want := memberAttrs["account_id"], testMemberAcct; got != want {
		t.Fatalf("member account_id = %#v, want %q", got, want)
	}

	rel := relationshipByType(t, envelopes, awscloud.RelationshipDetectiveGraphHasMemberAccount)
	if got := rel.Payload["source_resource_id"]; got != testGraphARN {
		t.Fatalf("member edge source_resource_id = %#v, want graph ARN %q", got, testGraphARN)
	}
	if got := rel.Payload["target_resource_id"]; got != testMemberAcct {
		t.Fatalf("member edge target_resource_id = %#v, want bare account id %q", got, testMemberAcct)
	}
	if got := rel.Payload["target_type"]; got != awscloud.ResourceTypeOrganizationsAccount {
		t.Fatalf("member edge target_type = %#v, want %q", got, awscloud.ResourceTypeOrganizationsAccount)
	}
}

func TestScannerEmitsGuardDutyDetectorEdgeWhenResolvable(t *testing.T) {
	client := fakeClient{graphs: []Graph{{
		ARN:                 testGraphARN,
		CreatedAt:           "2026-05-27T12:00:00Z",
		GuardDutyDetectorID: testDetectorID,
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	rel := relationshipByType(t, envelopes, awscloud.RelationshipDetectiveGraphSourcesGuardDutyDetector)
	if got := rel.Payload["source_resource_id"]; got != testGraphARN {
		t.Fatalf("detector edge source_resource_id = %#v, want graph ARN %q", got, testGraphARN)
	}
	if got := rel.Payload["target_resource_id"]; got != testDetectorID {
		t.Fatalf("detector edge target_resource_id = %#v, want bare detector id %q", got, testDetectorID)
	}
	if got := rel.Payload["target_type"]; got != awscloud.ResourceTypeGuardDutyDetector {
		t.Fatalf("detector edge target_type = %#v, want %q", got, awscloud.ResourceTypeGuardDutyDetector)
	}
}

func TestScannerOmitsGuardDutyDetectorEdgeWhenUnresolvable(t *testing.T) {
	// Detective's metadata APIs never report a detector id, so the common path
	// has no detector id and the edge must be omitted rather than dangled.
	client := fakeClient{graphs: []Graph{{ARN: testGraphARN}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == awscloud.RelationshipDetectiveGraphSourcesGuardDutyDetector {
			t.Fatalf("unexpected GuardDuty detector edge with no resolvable detector id: %s", mustJSON(t, envelope))
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	client := fakeClient{graphs: []Graph{{
		ARN:                 testGraphARN,
		GuardDutyDetectorID: testDetectorID,
	}}, members: map[string][]MemberAccount{
		testGraphARN: {{AccountID: testMemberAcct, GraphARN: testGraphARN, Status: "ENABLED"}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relguard.AssertObservations(t, observationsFromEnvelopes(t, envelopes)...)
}

func TestScannerMemberResourceIDIsStableAcrossOrder(t *testing.T) {
	// Two members in either order must keep their resource ids; the id is keyed
	// on graph ARN + account id, never on list index.
	first := []MemberAccount{
		{AccountID: "111122223333", GraphARN: testGraphARN, Status: "ENABLED"},
		{AccountID: "444455556666", GraphARN: testGraphARN, Status: "INVITED"},
	}
	second := []MemberAccount{first[1], first[0]}

	idsForOrder := func(members []MemberAccount) map[string]string {
		client := fakeClient{graphs: []Graph{{ARN: testGraphARN}}, members: map[string][]MemberAccount{testGraphARN: members}}
		envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		ids := map[string]string{}
		for _, envelope := range envelopes {
			if envelope.FactKind != facts.AWSResourceFactKind {
				continue
			}
			if envelope.Payload["resource_type"] != awscloud.ResourceTypeDetectiveMemberAccount {
				continue
			}
			attrs := attributesOf(t, envelope)
			ids[attrs["account_id"].(string)] = envelope.Payload["resource_id"].(string)
		}
		return ids
	}

	if got, want := idsForOrder(first), idsForOrder(second); !reflect.DeepEqual(got, want) {
		t.Fatalf("member resource ids changed with list order: %#v vs %#v", got, want)
	}
}

func TestScannerNeverEmitsInvestigationOrEmailData(t *testing.T) {
	client := fakeClient{graphs: []Graph{{ARN: testGraphARN}}, members: map[string][]MemberAccount{
		testGraphARN: {{AccountID: testMemberAcct, GraphARN: testGraphARN, Status: "ENABLED"}},
	}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	member := resourceByType(t, envelopes, awscloud.ResourceTypeDetectiveMemberAccount)
	memberAttrs := attributesOf(t, member)
	for _, forbidden := range []string{
		"email", "email_address", "emailaddress",
		"investigation", "investigations", "indicators",
		"finding_group", "finding_groups", "volume_usage_in_bytes",
		"percent_of_graph_utilization",
	} {
		if _, exists := memberAttrs[forbidden]; exists {
			t.Fatalf("%s attribute persisted; Detective scanner is metadata-only", forbidden)
		}
	}
	if strings.Contains(mustJSON(t, envelopes), testMemberEmail) {
		t.Fatalf("member email leaked into emitted facts: %s", mustJSON(t, envelopes))
	}
}

// TestScannerClientInterfaceExcludesInvestigationAndMutationAPIs is the metadata
// exclusion gate at the scanner-port boundary: the Client interface the scanner
// depends on must expose only the three safe list reads and no investigation,
// indicator, datasource-detail, or mutation operation. Reflecting over the
// interface fails the build if a forbidden method becomes reachable.
func TestScannerClientInterfaceExcludesInvestigationAndMutationAPIs(t *testing.T) {
	allowed := map[string]struct{}{
		"ListGraphs":  {},
		"ListMembers": {},
		"ListTags":    {},
	}
	forbiddenSubstrings := []string{
		"Investigation", "Indicator", "Datasource", "GetMembers",
		"Create", "Delete", "Update", "Tag", "Untag", "Put",
		"Accept", "Reject", "Disassociate", "Enable", "Disable",
		"Start", "Stop", "Monitoring",
	}
	iface := reflect.TypeOf((*Client)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("Client interface has no methods; expected the Detective read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if _, ok := allowed[name]; ok {
			// The three allow-set reads are proven safe by name and signature.
			// The forbidden-substring scan below runs only against methods
			// outside the allow-set, so the safe tag read (ListTags) is not
			// mis-flagged by the "Tag" mutation token.
			continue
		}
		t.Fatalf("Client exposes method %q outside the metadata-only allow-set", name)
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if _, ok := allowed[name]; ok {
			continue
		}
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("Client method %q contains forbidden operation %q; Detective scanner is metadata-only", name, banned)
			}
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceGuardDuty

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing client error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           testAdminAcct,
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceDetective,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:detective:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 12, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	graphs  []Graph
	members map[string][]MemberAccount
	tags    map[string]map[string]string
	err     error
}

func (c fakeClient) ListGraphs(context.Context) ([]Graph, error) {
	return c.graphs, c.err
}

func (c fakeClient) ListMembers(_ context.Context, graphARN string) ([]MemberAccount, error) {
	return c.members[graphARN], nil
}

func (c fakeClient) ListTags(_ context.Context, graphARN string) (map[string]string, error) {
	return c.tags[graphARN], nil
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
	t.Fatalf("missing resource_type %q in %s", resourceType, mustJSON(t, envelopes))
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
	t.Fatalf("missing relationship_type %q in %s", relationshipType, mustJSON(t, envelopes))
	return facts.Envelope{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

// observationsFromEnvelopes rebuilds RelationshipObservations from emitted
// relationship envelopes so relguard.AssertObservations can enforce the runtime
// graph-join contract on the exact target_type / target_resource_id pairs the
// scanner published.
func observationsFromEnvelopes(t *testing.T, envelopes []facts.Envelope) []awscloud.RelationshipObservation {
	t.Helper()
	var observations []awscloud.RelationshipObservation
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		observation := awscloud.RelationshipObservation{
			RelationshipType: stringField(envelope.Payload, "relationship_type"),
			SourceResourceID: stringField(envelope.Payload, "source_resource_id"),
			SourceARN:        stringField(envelope.Payload, "source_arn"),
			TargetResourceID: stringField(envelope.Payload, "target_resource_id"),
			TargetARN:        stringField(envelope.Payload, "target_arn"),
			TargetType:       stringField(envelope.Payload, "target_type"),
		}
		observations = append(observations, observation)
	}
	if len(observations) == 0 {
		t.Fatalf("no relationship envelopes to assert in %s", mustJSON(t, envelopes))
	}
	return observations
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(raw)
}
