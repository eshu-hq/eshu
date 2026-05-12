package ociregistry

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestWarningEnvelopeRedactsURLsAndCredentials(t *testing.T) {
	t.Parallel()

	observation := WarningObservation{
		WarningKey:          "referrers:team/api",
		WarningCode:         WarningUnsupportedReferrersAPI,
		Severity:            SeverityInfo,
		Message:             "GET https://user:secret@jfrog.example/v2/team/api/referrers/" + sha256Digest + "?token=secret returned 404",
		Repository:          &RepositoryIdentity{Provider: ProviderJFrog, Registry: "jfrog.example", Repository: "team/api"},
		Digest:              sha256Digest,
		GenerationID:        "generation-warning",
		CollectorInstanceID: "jfrog-docker",
		FencingToken:        21,
		ObservedAt:          time.Date(2026, 5, 12, 12, 15, 0, 0, time.UTC),
		SourceURI:           "https://user:secret@jfrog.example/v2/team/api/referrers/" + sha256Digest + "?x-amz-signature=secret",
	}

	envelope, err := NewWarningEnvelope(observation)
	if err != nil {
		t.Fatalf("NewWarningEnvelope() error = %v", err)
	}

	assertOCIEnvelope(t, envelope, facts.OCIRegistryWarningFactKind, facts.OCIRegistryWarningSchemaVersion)
	if got := envelope.Payload["warning_code"]; got != WarningUnsupportedReferrersAPI {
		t.Fatalf("warning_code = %#v", got)
	}
	if got := envelope.Payload["digest"]; got != sha256Digest {
		t.Fatalf("digest = %#v", got)
	}
	message, ok := envelope.Payload["message"].(string)
	if !ok {
		t.Fatalf("message = %#v", envelope.Payload["message"])
	}
	for _, leaked := range []string{"user:secret", "token=secret", "x-amz-signature"} {
		if strings.Contains(message, leaked) || strings.Contains(envelope.SourceRef.SourceURI, leaked) {
			t.Fatalf("warning leaked %q in message=%q source_uri=%q", leaked, message, envelope.SourceRef.SourceURI)
		}
	}
	if got := envelope.Payload["referrers_state"]; got != ReferrersUnsupported {
		t.Fatalf("referrers_state = %#v", got)
	}
}

func TestWarningEnvelopeRejectsBlankWarningCode(t *testing.T) {
	t.Parallel()

	_, err := NewWarningEnvelope(WarningObservation{
		WarningKey:          "blank-code",
		GenerationID:        "generation-warning",
		CollectorInstanceID: "jfrog-docker",
	})
	if err == nil {
		t.Fatalf("NewWarningEnvelope(blank code) error = nil, want non-nil")
	}
}
