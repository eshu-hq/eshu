package freshness

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestTriggerValidateAcceptsBoundedTarget(t *testing.T) {
	t.Parallel()

	trigger := Trigger{
		EventID:      "evt-123",
		Kind:         EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-east-1",
		ServiceKind:  awscloud.ServiceLambda,
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		ResourceID:   "function-1",
		ObservedAt:   time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
	}

	if err := trigger.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if got, want := trigger.Target().ScopeID(), "aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("ScopeID() = %q, want %q", got, want)
	}
	encoded, err := trigger.Target().AcceptanceUnitID()
	if err != nil {
		t.Fatalf("AcceptanceUnitID() error = %v, want nil", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("AcceptanceUnitID() JSON decode error = %v", err)
	}
	if got, want := decoded["service_kind"], awscloud.ServiceLambda; got != want {
		t.Fatalf("service_kind = %q, want %q", got, want)
	}
}

func TestTriggerValidateRejectsWildcardAndUnknownService(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		trigger Trigger
		want    string
	}{
		{
			name: "wildcard region",
			trigger: Trigger{
				EventID:     "evt-1",
				Kind:        EventKindConfigChange,
				AccountID:   "123456789012",
				Region:      "*",
				ServiceKind: awscloud.ServiceLambda,
				ObservedAt:  observedAt,
			},
			want: "region must not contain wildcard",
		},
		{
			name: "unknown service",
			trigger: Trigger{
				EventID:     "evt-2",
				Kind:        EventKindCloudTrailAPI,
				AccountID:   "123456789012",
				Region:      "us-east-1",
				ServiceKind: "unknown-service",
				ObservedAt:  observedAt,
			},
			want: "unsupported service_kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.trigger.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("Validate() error = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestStoredTriggerKeysCoalesceByTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	first := Trigger{
		EventID:      "evt-1",
		Kind:         EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-east-1",
		ServiceKind:  awscloud.ServiceLambda,
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		ResourceID:   "function-a",
		ObservedAt:   observedAt,
	}
	second := first
	second.EventID = "evt-2"
	second.ResourceID = "function-b"

	firstStored, err := NewStoredTrigger(first, observedAt)
	if err != nil {
		t.Fatalf("NewStoredTrigger(first) error = %v, want nil", err)
	}
	secondStored, err := NewStoredTrigger(second, observedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("NewStoredTrigger(second) error = %v, want nil", err)
	}
	if firstStored.FreshnessKey != secondStored.FreshnessKey {
		t.Fatalf("FreshnessKey differs for same target: %q vs %q", firstStored.FreshnessKey, secondStored.FreshnessKey)
	}
	if firstStored.DeliveryKey == secondStored.DeliveryKey {
		t.Fatalf("DeliveryKey = %q, want per-event delivery keys to differ", firstStored.DeliveryKey)
	}
}
