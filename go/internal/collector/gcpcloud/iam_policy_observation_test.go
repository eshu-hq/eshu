package gcpcloud

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testIAMPolicyObservation() IAMPolicyObservation {
	return IAMPolicyObservation{
		Boundary:         testBoundary(),
		FullResourceName: "//storage.googleapis.com/projects/_/buckets/my-bucket",
		AssetType:        "storage.googleapis.com/Bucket",
		Role:             "roles/storage.admin",
		Members:          []string{"user:alice@example.com", "serviceAccount:svc@my-project.iam.gserviceaccount.com"},
		ConditionPresent: true,
		Etag:             "etag-abc",
	}
}

// TestNewIAMPolicyObservationEnvelopeFingerprintsMembers proves each member is
// recorded as a class plus a keyed fingerprint (never the raw member), the role
// and condition presence are kept, and no raw policy JSON is carried.
func TestNewIAMPolicyObservationEnvelopeFingerprintsMembers(t *testing.T) {
	obs := testIAMPolicyObservation()
	key := testRedactionKey(t)

	env, err := NewIAMPolicyObservationEnvelope(obs, key)
	if err != nil {
		t.Fatalf("NewIAMPolicyObservationEnvelope error: %v", err)
	}
	if env.FactKind != facts.GCPIAMPolicyObservationFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	if env.Payload["role"] != "roles/storage.admin" {
		t.Fatalf("role = %#v", env.Payload["role"])
	}
	members, ok := env.Payload["members"].([]map[string]string)
	if !ok || len(members) != 2 {
		t.Fatalf("members = %#v, want 2", env.Payload["members"])
	}
	for _, m := range members {
		if m["member_class"] == "" || m["member_fingerprint"] == "" {
			t.Fatalf("member missing class/fingerprint: %#v", m)
		}
		if m["member_fingerprint"] == "alice@example.com" || m["member_fingerprint"] == "user:alice@example.com" {
			t.Fatalf("raw member leaked: %#v", m)
		}
	}
	if env.Payload["condition_present"] != true {
		t.Fatalf("condition_present = %#v", env.Payload["condition_present"])
	}
	// No raw policy JSON field.
	if _, present := env.Payload["policy"]; present {
		t.Fatalf("raw policy carried: %#v", env.Payload["policy"])
	}
}

// TestNewIAMPolicyObservationEnvelopeRejectsInvalid proves the builder fails
// closed on a missing resource name, asset type, role, no members, or zero key.
func TestNewIAMPolicyObservationEnvelopeRejectsInvalid(t *testing.T) {
	key := testRedactionKey(t)
	for name, mutate := range map[string]func(*IAMPolicyObservation){
		"missing name":  func(o *IAMPolicyObservation) { o.FullResourceName = "" },
		"missing asset": func(o *IAMPolicyObservation) { o.AssetType = "" },
		"missing role":  func(o *IAMPolicyObservation) { o.Role = "" },
		"no members":    func(o *IAMPolicyObservation) { o.Members = []string{"   "} },
	} {
		obs := testIAMPolicyObservation()
		mutate(&obs)
		if _, err := NewIAMPolicyObservationEnvelope(obs, key); err == nil {
			t.Fatalf("%s: error = nil, want non-nil", name)
		}
	}
	if _, err := NewIAMPolicyObservationEnvelope(testIAMPolicyObservation(), redact.Key{}); err == nil {
		t.Fatal("zero key: error = nil, want non-nil")
	}
}
