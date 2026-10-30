// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ecsBindingScaleCensus builds a shim-scale census: 187 declarations
// across 32 repos with one bindable service per the first 40 entries.
func ecsBindingScaleCensus(services int) (ECSBindingCensusResult, []facts.Envelope) {
	const digest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	declarations := make([]ECSBindingDeclaration, 0, 187)
	envelopes := make([]facts.Envelope, 0, 2*services)
	for i := 0; i < services; i++ {
		name := fmt.Sprintf("svc-%03d", i)
		repo := fmt.Sprintf("repo-%02d", i%32)
		clusterARN := "arn:aws:ecs:us-east-1:111122223333:cluster/prod"
		declarations = append(declarations,
			ecsBindingDeclaration(repo, "main.tf", name, `"`+name+`"`, `"`+clusterARN+`"`, nil))
		envelopes = append(envelopes,
			ecsBindingServiceEnvelope(fmt.Sprintf("fact-svc-%d", i), name, "prod", map[string]any{"environment": "prod"}),
			ecsBindingTaskEnvelope(fmt.Sprintf("fact-task-%d", i), name, "prod", []string{digest}))
	}
	for i := len(declarations); i < 187; i++ {
		name := fmt.Sprintf("other-%03d", i)
		declarations = append(declarations,
			ecsBindingDeclaration(fmt.Sprintf("repo-%02d", i%32), "other.tf", name,
				`"other-cluster-svc"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/other"`, nil))
	}
	return ECSBindingCensusResult{Declarations: declarations}, envelopes
}

func BenchmarkExtractWorkloadECSBindingMatrix(b *testing.B) {
	census := ecsBindingMatrixCensus()
	census.Declarations = append(census.Declarations, ECSBindingDeclaration{
		RepoID: "repo-app", Path: "main.tf",
		EntityType: "TerraformVariable", EntityName: "service_name",
		Metadata: map[string]any{"name": "service_name", "default": `"api"`},
	})
	envelopes := ecsBindingMatrixEnvelopes()
	instances := map[string][]string{
		"api": {"prod"}, "web": {"prod"}, "single": {"staging"}, "conflict": {"staging"},
		"noenv": {"prod", "staging"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := ExtractWorkloadECSBindingRows(envelopes, census, instances); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExtractWorkloadECSBindingScale(b *testing.B) {
	census, envelopes := ecsBindingScaleCensus(40)
	instances := make(map[string][]string, 40)
	for i := 0; i < 40; i++ {
		instances[fmt.Sprintf("svc-%03d", i)] = []string{"prod"}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _, _, err := ExtractWorkloadECSBindingRows(envelopes, census, instances)
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) != 40 {
			b.Fatalf("bound rows = %d, want 40", len(rows))
		}
	}
}
