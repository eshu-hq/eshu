package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkSecretsIAMGCPGrantObservations measures the in-memory projection of
// GCP IAM principal/permission source facts into privilege-posture observations
// for a realistic per-scope-generation grant count. It must stay O(P+G) with no
// per-grant graph or database round trip.
func BenchmarkSecretsIAMGCPGrantObservations(b *testing.B) {
	const principalCount = 2000
	envelopes := make([]facts.Envelope, 0, principalCount*3)
	for i := 0; i < principalCount; i++ {
		fp := fmt.Sprintf("sha256:svc-%d", i)
		envelopes = append(
			envelopes,
			gcpPrincipalFact(fp),
			gcpPermissionFact(fmt.Sprintf("perm-secret-%d", i), fp, "roles/secretmanager.secretAccessor", true, false),
			gcpPermissionFact(fmt.Sprintf("perm-owner-%d", i), fp, "roles/owner", false, true),
		)
	}
	index := buildSecretsIAMIndex(envelopes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observations := secretsIAMGCPGrantObservations(index)
		if len(observations) != principalCount*2 {
			b.Fatalf("observations = %d, want %d", len(observations), principalCount*2)
		}
	}
}
