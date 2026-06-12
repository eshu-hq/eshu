package gcpcloud

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testTagObservation() TagObservation {
	return TagObservation{
		Boundary:         testBoundary(),
		FullResourceName: "//compute.googleapis.com/projects/my-project/zones/us-central1-a/instances/api-1",
		AssetType:        "compute.googleapis.com/Instance",
		Tags:             map[string]string{"env": "prod", "team": "platform"},
		SourceKind:       "direct",
	}
}

// TestNewTagObservationEnvelopeFingerprintsValues proves tag keys are preserved
// and every tag value is fingerprinted (never raw).
func TestNewTagObservationEnvelopeFingerprintsValues(t *testing.T) {
	obs := testTagObservation()
	key := testRedactionKey(t)

	env, err := NewTagObservationEnvelope(obs, key)
	if err != nil {
		t.Fatalf("NewTagObservationEnvelope error: %v", err)
	}
	if env.FactKind != facts.GCPTagObservationFactKind {
		t.Fatalf("FactKind = %q", env.FactKind)
	}
	fps, ok := env.Payload["tag_value_fingerprints"].(map[string]string)
	if !ok || len(fps) != 2 {
		t.Fatalf("tag_value_fingerprints = %#v", env.Payload["tag_value_fingerprints"])
	}
	for k, marker := range fps {
		if marker == "prod" || marker == "platform" {
			t.Fatalf("raw tag value leaked for key %q: %q", k, marker)
		}
	}
	keys, ok := env.Payload["tag_keys"].([]string)
	if !ok || len(keys) != 2 {
		t.Fatalf("tag_keys = %#v, want [env team]", env.Payload["tag_keys"])
	}
}

// TestNewTagObservationEnvelopeRejectsInvalid proves the builder fails closed on a
// missing resource name, asset type, no usable tags, or a zero redaction key.
func TestNewTagObservationEnvelopeRejectsInvalid(t *testing.T) {
	key := testRedactionKey(t)
	for name, mutate := range map[string]func(*TagObservation){
		"missing name":  func(o *TagObservation) { o.FullResourceName = "" },
		"missing asset": func(o *TagObservation) { o.AssetType = "" },
		"no tags":       func(o *TagObservation) { o.Tags = map[string]string{"  ": "x"} },
	} {
		obs := testTagObservation()
		mutate(&obs)
		if _, err := NewTagObservationEnvelope(obs, key); err == nil {
			t.Fatalf("%s: error = nil, want non-nil", name)
		}
	}
	if _, err := NewTagObservationEnvelope(testTagObservation(), redact.Key{}); err == nil {
		t.Fatal("zero key: error = nil, want non-nil")
	}
}
