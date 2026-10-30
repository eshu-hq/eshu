// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
)

const ecsBindingTestAccount = "111122223333"

func ecsBindingServiceARN(cluster, name string) string {
	return "arn:aws:ecs:us-east-1:" + ecsBindingTestAccount + ":service/" + cluster + "/" + name
}

func ecsBindingClusterARN(cluster string) string {
	return "arn:aws:ecs:us-east-1:" + ecsBindingTestAccount + ":cluster/" + cluster
}

// ecsBindingServiceEnvelope builds an aws_ecs_service fact payload in the
// shape the ECS scanner emits: ARN identity, cluster_arn/running_count
// attributes, and optional flat anchor tags.
func ecsBindingServiceEnvelope(factID, name, cluster string, extra map[string]any) facts.Envelope {
	payload := map[string]any{
		"account_id":    ecsBindingTestAccount,
		"region":        "us-east-1",
		"resource_type": "aws_ecs_service",
		"resource_id":   ecsBindingServiceARN(cluster, name),
		"arn":           ecsBindingServiceARN(cluster, name),
		"name":          name,
		"state":         "ACTIVE",
		"attributes": map[string]any{
			"cluster_arn":   ecsBindingClusterARN(cluster),
			"running_count": 1,
		},
	}
	for key, value := range extra {
		payload[key] = value
	}
	return workloadCloudAWSResourceEnvelope(factID, payload)
}

// ecsBindingTaskEnvelope builds an aws_ecs_task fact payload for one
// RUNNING task of service name on cluster with the given container digests.
func ecsBindingTaskEnvelope(factID, name, cluster string, digests []string) facts.Envelope {
	containers := make([]any, 0, len(digests))
	for i, digest := range digests {
		containers = append(containers, map[string]any{
			"name":         name,
			"image":        ecsBindingTestAccount + ".dkr.ecr.us-east-1.amazonaws.com/" + name + ":latest",
			"image_digest": digest,
			"runtime_id":   "runtime-" + string(rune('a'+i)),
		})
	}
	return workloadCloudAWSResourceEnvelope(factID, map[string]any{
		"account_id":    ecsBindingTestAccount,
		"region":        "us-east-1",
		"resource_type": "aws_ecs_task",
		"resource_id":   "arn:aws:ecs:us-east-1:" + ecsBindingTestAccount + ":task/" + cluster + "/task-" + name,
		"arn":           "arn:aws:ecs:us-east-1:" + ecsBindingTestAccount + ":task/" + cluster + "/task-" + name,
		"name":          "task-" + name,
		"state":         "RUNNING",
		"attributes": map[string]any{
			"cluster_arn": ecsBindingClusterARN(cluster),
			"group":       "service:" + name,
			"containers":  containers,
		},
	})
}

// ecsBindingDeclaration builds a census TerraformResource declaration for
// an aws_ecs_service with the given raw name/cluster expressions.
func ecsBindingDeclaration(repoID, path, name, rawName, rawCluster string, extra map[string]any) ECSBindingDeclaration {
	metadata := map[string]any{
		"resource_type": "aws_ecs_service",
		"resource_name": name,
		"attributes":    map[string]any{"name": rawName, "cluster": rawCluster},
	}
	for key, value := range extra {
		metadata[key] = value
	}
	return ECSBindingDeclaration{
		RepoID: repoID, Path: path,
		EntityType: "TerraformResource", EntityName: "aws_ecs_service." + name,
		Metadata: metadata,
	}
}

// ecsBindingMatrixCensus is the >10-service spot-check corpus: twelve
// services exercising every matrix leg. See TestExtractWorkloadECSBindingMatrix.
func ecsBindingMatrixCensus() ECSBindingCensusResult {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_ = digest
	return ECSBindingCensusResult{
		Declarations: []ECSBindingDeclaration{
			// api: var-default name, literal cluster, own repo (positive).
			ecsBindingDeclaration("repo-app", "main.tf", "api", "var.service_name", `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// web: literal declaration in a different repo than the workload (cross-repo positive).
			ecsBindingDeclaration("repo-infra", "services.tf", "web", `"web"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// dup: same (name, cluster) claimed by two repos (duplicate).
			ecsBindingDeclaration("repo-app", "dup.tf", "dup", `"dup"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			ecsBindingDeclaration("repo-infra", "dup.tf", "dup", `"dup"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// shared: same name in two clusters (shared-name).
			ecsBindingDeclaration("repo-app", "shared.tf", "shared", `"shared"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			ecsBindingDeclaration("repo-app", "shared-staging.tf", "shared_staging", `"shared"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/staging"`, nil),
			// foreach-svc: for_each with a literal name (repeat-unresolved).
			ecsBindingDeclaration("repo-app", "fleet.tf", "fleet", `"foreach-svc"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, map[string]any{
				"for_each": `var.fleet`,
			}),
			// ghost: declaration with no live service (unobserved; never visited).
			ecsBindingDeclaration("repo-app", "ghost.tf", "ghost", `"ghost"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// nocorro: live service but no running task facts (uncorroborated).
			ecsBindingDeclaration("repo-app", "nocorro.tf", "nocorro", `"nocorro"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// noenv: live + corroborated but no environment anywhere, two instances (environment-missing).
			ecsBindingDeclaration("repo-app", "noenv.tf", "noenv", `"noenv"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// single: no env tags, exactly one instance (single-instance binds).
			ecsBindingDeclaration("repo-app", "single.tf", "single", `"single"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// conflict: fact env prod, single instance staging (environment-conflict).
			ecsBindingDeclaration("repo-app", "conflict.tf", "conflict", `"conflict"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// anchored: explicit workload anchor owns the service (anchor-path).
			ecsBindingDeclaration("repo-app", "anchored.tf", "anchored", `"anchored"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
			// lonely-svc: name resolves but the cluster stays unknown (unresolved).
			ecsBindingDeclaration("repo-app", "lonely.tf", "lonely", `"lonely-svc"`, "var.missing", nil),
		},
	}
}

// ecsBindingMatrixEnvelopes builds the live AWS-scope facts for the matrix.
func ecsBindingMatrixEnvelopes() []facts.Envelope {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	live := []string{"api", "web", "dup", "shared", "foreach-svc", "nocorro", "noenv", "single", "conflict", "anchored", "lonely-svc"}
	envelopes := make([]facts.Envelope, 0, 2*len(live))
	for _, name := range live {
		cluster := "prod"
		if name == "shared" {
			// Only the prod-cluster shared service is live; the staging
			// declaration stays a candidate poison, never a bind.
			envelopes = append(envelopes, ecsBindingServiceEnvelope("fact-svc-"+name+"-prod", name, cluster, map[string]any{"environment": "prod"}))
			envelopes = append(envelopes, ecsBindingTaskEnvelope("fact-task-"+name+"-prod", name, cluster, []string{digest}))
			continue
		}
		extra := map[string]any{"environment": "prod"}
		switch name {
		case "nocorro":
			// Live but no task facts at all.
			envelopes = append(envelopes, ecsBindingServiceEnvelope("fact-svc-"+name, name, cluster, extra))
			continue
		case "noenv", "single":
			extra = nil
		case "anchored":
			extra["workload_id"] = "workload:anchored"
		}
		envelopes = append(envelopes, ecsBindingServiceEnvelope("fact-svc-"+name, name, cluster, extra))
		envelopes = append(envelopes, ecsBindingTaskEnvelope("fact-task-"+name, name, cluster, []string{digest}))
	}
	return envelopes
}

func ecsBindingRowsByService(rows []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		out[anyToString(row["cloud_resource_uid"])] = row
	}
	return out
}

func TestExtractWorkloadECSBindingMatrix(t *testing.T) {
	t.Parallel()

	census := ecsBindingMatrixCensus()
	// The api declaration resolves its name through this same-dir default.
	census.Declarations = append(census.Declarations, ECSBindingDeclaration{
		RepoID: "repo-app", Path: "main.tf",
		EntityType: "TerraformVariable", EntityName: "service_name",
		Metadata: map[string]any{"name": "service_name", "default": `"api"`},
	})
	instances := map[string][]string{
		"api": {"prod"}, "web": {"prod"}, "single": {"staging"}, "conflict": {"staging"},
		"noenv": {"prod", "staging"},
	}

	rows, tally, _, err := ExtractWorkloadECSBindingRows(ecsBindingMatrixEnvelopes(), census, instances)
	if err != nil {
		t.Fatalf("ExtractWorkloadECSBindingRows returned error: %v", err)
	}
	byService := ecsBindingRowsByService(rows)

	// Positive: api (module-computed via variable default) and web
	// (cross-repo literal) bind with the declaration basis and count.
	for _, name := range []string{"api", "web"} {
		row, ok := byService[ecsBindingCloudUID(name)]
		if !ok {
			t.Fatalf("matrix: service %q has no binding row; rows=%d tally=%#v", name, len(rows), tally.skipped)
		}
		if got := anyToString(row["relationship_basis"]); got != ecsBindingBasisDeclaration {
			t.Errorf("matrix: service %q basis = %q, want %q", name, got, ecsBindingBasisDeclaration)
		}
		if got := anyToString(row["workload_id"]); got != name {
			t.Errorf("matrix: service %q workload_id = %q, want bare name %q", name, got, name)
		}
		if got := anyToString(row["environment"]); got == "" {
			t.Errorf("matrix: service %q has empty environment", name)
		}
	}
	// Positive: single binds through its sole instance environment.
	if row, ok := byService[ecsBindingCloudUID("single")]; !ok {
		t.Fatalf("matrix: single-instance service has no binding row; tally=%#v", tally.skipped)
	} else if got := anyToString(row["environment"]); got != "staging" {
		t.Errorf("matrix: single environment = %q, want staging", got)
	}
	if len(rows) != 3 {
		t.Fatalf("matrix: bound %d services, want exactly 3 (api, web, single); tally=%#v", len(rows), tally.skipped)
	}

	// Every ambiguous/negative leg fails closed with its named reason.
	wantSkips := map[string]int{
		ecsBindingSkipDuplicateDeclaration:  1, // dup x2 claims, one live service
		ecsBindingSkipSharedServiceName:     1, // shared prod live, staging declaration poisons
		ecsBindingSkipRepeatUnresolved:      1, // foreach-svc for_each
		ecsBindingSkipAnchorOwned:           1, // anchored explicit workload anchor
		ecsBindingSkipUncorroborated:        1, // nocorro live but no running tasks
		ecsBindingSkipEnvironmentMissing:    1, // noenv two instances, no env signal
		ecsBindingSkipEnvironmentConflict:   1, // conflict fact prod vs instance staging
		ecsBindingSkipDeclarationUnresolved: 1, // lonely-svc cluster var.missing
		ecsBindingSkipServiceUnobserved:     0, // ghost never visited (no live fact)
		ecsBindingSkipDeclarationMissing:    0, // every live service has a declaration here
	}
	for reason, want := range wantSkips {
		if got := tally.skipped[reason]; got != want {
			t.Errorf("matrix: skip %q = %d, want %d (full tally=%#v)", reason, got, want, tally.skipped)
		}
	}
}

func ecsBindingCloudUID(name string) string {
	return cloudjoin.CloudResourceUID(ecsBindingTestAccount, "us-east-1", "aws_ecs_service", ecsBindingServiceARN("prod", name))
}

func TestExtractWorkloadECSBindingTfstateScaffold(t *testing.T) {
	t.Parallel()

	// The tfstate scaffold reads applied identity from a
	// terraform_state_resource fact. Producer coverage is zero today, so
	// this synthetic envelope proves the code path, not live data.
	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	envelopes := []facts.Envelope{
		ecsBindingServiceEnvelope("fact-svc-tf", "tf", "prod", map[string]any{"environment": "prod"}),
		ecsBindingTaskEnvelope("fact-task-tf", "tf", "prod", []string{digest}),
		{
			FactID:        "fact-tfstate-1",
			StableFactKey: "stable-fact-tfstate-1",
			FactKind:      "terraform_state_resource",
			Payload: map[string]any{
				"address":       "aws_ecs_service.tf",
				"resource_type": "aws_ecs_service",
				"attributes": map[string]any{
					"name":    "tf",
					"cluster": "arn:aws:ecs:us-east-1:111122223333:cluster/prod",
				},
			},
			SourceRef:     facts.Ref{SourceSystem: "terraform-state", SourceRecordID: "tfstate-1"},
			CollectorKind: "terraform_state",
		},
	}
	rows, tally, _, err := ExtractWorkloadECSBindingRows(envelopes, ECSBindingCensusResult{}, map[string][]string{"tf": {"prod"}})
	if err != nil {
		t.Fatalf("ExtractWorkloadECSBindingRows returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("tfstate scaffold: bound %d services, want 1; tally=%#v", len(rows), tally.skipped)
	}
	if got := anyToString(rows[0]["relationship_basis"]); got != ecsBindingBasisTfstate {
		t.Errorf("tfstate scaffold: basis = %q, want %q", got, ecsBindingBasisTfstate)
	}
}

func TestExtractWorkloadECSBindingTallyFollowsImageTally(t *testing.T) {
	t.Parallel()

	// The tally carries a resolved count plus per-reason skips, mirroring
	// awsCloudImageEdgeTally: an operator can answer "why is service X
	// unbound" from the evaluation summary without a per-edge log line.
	rows, tally, _, err := ExtractWorkloadECSBindingRows(
		ecsBindingMatrixEnvelopes(), ecsBindingMatrixCensus(),
		map[string][]string{"api": {"prod"}},
	)
	if err != nil {
		t.Fatalf("ExtractWorkloadECSBindingRows returned error: %v", err)
	}
	_ = rows
	total := tally.totalSkipped()
	if total == 0 {
		t.Fatalf("matrix tally has no skips; tally=%#v", tally.skipped)
	}
	if len(tally.skipped) < 5 {
		t.Fatalf("matrix tally has %d distinct reasons, want >= 5; tally=%#v", len(tally.skipped), tally.skipped)
	}
}

func TestExtractWorkloadECSBindingNoServicesSkipsCensus(t *testing.T) {
	t.Parallel()

	// Scopes without ECS facts (every repo scope) short-circuit before any
	// census or lookup work: the census is never even consulted.
	rows, tally, _, err := ExtractWorkloadECSBindingRows(
		[]facts.Envelope{workloadCloudAWSResourceEnvelope("fact-other", map[string]any{
			"account_id":    ecsBindingTestAccount,
			"region":        "us-east-1",
			"resource_type": "aws_s3_bucket",
			"resource_id":   "bucket",
		})},
		ECSBindingCensusResult{Declarations: []ECSBindingDeclaration{
			ecsBindingDeclaration("repo-app", "main.tf", "api", `"api"`, `"arn:aws:ecs:us-east-1:111122223333:cluster/prod"`, nil),
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("ExtractWorkloadECSBindingRows returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("non-ECS scope bound %d rows, want 0", len(rows))
	}
	_ = tally
}
