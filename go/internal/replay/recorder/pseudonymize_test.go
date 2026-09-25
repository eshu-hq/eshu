// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recorder_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/recorder"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Every literal below is synthetic: the account is the private-data gate's
// own planted sample, the zone and names are invented for this test.
const (
	rawAccount   = "210987654321"
	rawZone      = "internal-zone-demo.com"
	rawRepo      = "payments-api"
	rawCluster   = "demo-cluster"
	rawRole      = "payments-deployer"
	rawTaskID    = "0123456789abcdef0123456789abcdef"
	rawIP        = "10.20.30.40"
	rawSubnet    = "subnet-0a1b2c3d4e5f60718"
	testKeyBytes = "6965-p3-test-key-0123456789abcdef0123456789abcdef0123456789abcdef"
)

func testPseudonymKey(t *testing.T) recordpseudo.Key {
	t.Helper()
	key, err := recordpseudo.NewKey([]byte(testKeyBytes))
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return key
}

// awsShapedGeneration is one AWS-looking scope carrying a real-shaped
// account, ARNs, an ECR image reference, a private IPv4 address, an AWS-issued
// subnet id and an organisation hostname -- one of every class the pilot must
// pseudonymize before a cassette can be written.
func awsShapedGeneration() collector.CollectedGeneration {
	observedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	scopeID := "aws:" + rawAccount + ":us-east-1:ecs"
	s := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "aws",
		ScopeKind:     scope.KindRegion,
		CollectorKind: scope.CollectorAWS,
		PartitionKey:  rawAccount + ":us-east-1:ecs",
		Metadata:      map[string]string{"account_id": rawAccount, "region": "us-east-1", "service_kind": "ecs"},
	}
	g := scope.ScopeGeneration{
		GenerationID: "gen-1",
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	taskARN := "arn:aws:ecs:us-east-1:" + rawAccount + ":task/" + rawCluster + "/" + rawTaskID
	roleARN := "arn:aws:iam::" + rawAccount + ":role/" + rawRole
	image := rawAccount + ".dkr.ecr.us-east-1.amazonaws.com/" + rawRepo + ":1.4.2"
	envs := []facts.Envelope{
		{
			FactKind:         facts.AWSResourceFactKind,
			StableFactKey:    facts.StableID(facts.AWSResourceFactKind, map[string]any{"account_id": rawAccount, "region": "us-east-1", "resource_id": taskARN, "resource_type": "ecs.task"}),
			SchemaVersion:    "1",
			CollectorKind:    string(scope.CollectorAWS),
			FencingToken:     1,
			SourceConfidence: "reported",
			Payload: map[string]any{
				"account_id":    rawAccount,
				"region":        "us-east-1",
				"service_kind":  "ecs",
				"arn":           taskARN,
				"resource_id":   taskARN,
				"resource_type": "ecs.task",
				"name":          rawTaskID,
				"state":         "RUNNING",
				"attributes": map[string]any{
					"cluster_arn":   "arn:aws:ecs:us-east-1:" + rawAccount + ":cluster/" + rawCluster,
					"task_role_arn": roleARN,
					"launch_type":   "FARGATE",
					"containers": []any{
						map[string]any{"name": rawRepo, "image": image, "runtime_id": rawTaskID + "-1234567890"},
					},
					"network_interfaces": []any{
						map[string]any{"private_ipv4_address": rawIP, "subnet_id": rawSubnet},
					},
				},
				"correlation_anchors": []any{taskARN},
			},
			SourceRef: facts.Ref{SourceURI: "aws://ecs/" + rawAccount + "/us-east-1/task/" + rawTaskID, SourceRecordID: taskARN},
		},
		{
			FactKind:         facts.AWSDNSRecordFactKind,
			StableFactKey:    facts.StableID(facts.AWSDNSRecordFactKind, map[string]any{"account_id": rawAccount, "hosted_zone_id": "Z0123456789ABCDEFGHIJ", "record_name": "api.payments." + rawZone + ".", "record_type": "A"}),
			SchemaVersion:    "1",
			CollectorKind:    string(scope.CollectorAWS),
			FencingToken:     1,
			SourceConfidence: "reported",
			Payload: map[string]any{
				"account_id":             rawAccount,
				"region":                 "us-east-1",
				"service_kind":           "route53",
				"hosted_zone_id":         "Z0123456789ABCDEFGHIJ",
				"hosted_zone_name":       rawZone + ".",
				"record_name":            "api.payments." + rawZone + ".",
				"normalized_record_name": "api.payments." + rawZone,
				"record_type":            "A",
				"values":                 []any{"10.20.30.41"},
			},
			SourceRef: facts.Ref{SourceURI: "aws://route53/" + rawAccount + "/Z0123456789ABCDEFGHIJ", SourceRecordID: "api.payments." + rawZone + "."},
		},
	}
	return collector.FactsFromSlice(s, g, envs)
}

// rawTokens are the literals that must never reach the cassette.
func rawTokens() []string {
	return []string{rawAccount, rawZone, rawRepo, rawCluster, rawRole, rawTaskID, rawIP, rawSubnet, "10.20.30.41"}
}

// gateScan runs the committed private-data gate library over dir under a
// bash that supports it (>= 4.3), exactly as scripts/verify-cassette-author.sh
// does, and returns the exit code with the combined output.
func gateScan(t *testing.T, dir string) (int, string) {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	lib := filepath.Join(repoRoot, "scripts", "lib", "cassette_private_data_pattern.sh")
	if _, err := os.Stat(lib); err != nil {
		t.Fatalf("gate library missing: %v", err)
	}
	shell := ""
	for _, candidate := range []string{"/opt/homebrew/bin/bash", "/usr/local/bin/bash", "bash"} {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, "-c", `[[ "${BASH_VERSINFO[0]}" -gt 4 || ("${BASH_VERSINFO[0]}" -eq 4 && "${BASH_VERSINFO[1]}" -ge 3) ]]`).CombinedOutput()
		if err == nil {
			shell = path
			break
		}
		_ = out
	}
	if shell == "" {
		t.Skip("no bash >= 4.3 available for the private-data gate library")
	}
	// The gate library shells out to ripgrep and fails closed, naming the
	// missing tool, when rg is not on PATH; this test then FAILS on it. The
	// CI lanes that run go test on this package are test.yml's go-race
	// shards, macos.yml and code-coverage-report.yml, and all of them install
	// ripgrep (go-core also installs it but runs no go test on go/), so an
	// absent rg is a lane misconfiguration, not a reason to skip.
	script := `set -euo pipefail; fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }; source "$1"; cassette_private_data_scan "$2"`
	cmd := exec.Command(shell, "-c", script, "gate", lib, dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), string(out)
	}
	t.Fatalf("run gate: %v", err)
	return -1, ""
}

var pseudoAccountRe = regexp.MustCompile(`^aws:0000[0-9]{8}:us-east-1:ecs$`)

// TestRunPseudonymizesIdentifiersBeforeWrite is the pilot's core proof: a
// raw-identifier source recorded with Pseudonymize configured yields a
// cassette that carries none of the raw literals, passes the committed
// private-data gate, passes the record-time membership belt (Verify), and
// still replays with the same fact shape.
func TestRunPseudonymizesIdentifiersBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recorded.json")
	var report recordpseudo.Report
	key := testPseudonymKey(t)
	err := recorder.Run(context.Background(), &fakeSource{gens: []collector.CollectedGeneration{awsShapedGeneration()}}, recorder.Options{
		Path:           path,
		CollectorLabel: "aws",
		Pseudonymize: &recordpseudo.Config{
			Key:    key,
			Policy: recordpolicy.Policy(),
		},
		RequirePseudonymization: true,
		OnPseudonymized:         func(r recordpseudo.Report) { report = r },
	})
	if err != nil {
		t.Fatalf("recorder.Run: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	for _, raw := range rawTokens() {
		if bytes.Contains(out, []byte(raw)) {
			t.Errorf("raw token of length %d survived into the cassette", len(raw))
		}
	}
	if code, gateOut := gateScan(t, dir); code != 0 {
		t.Errorf("private-data gate exit %d on the recorded cassette:\n%s", code, gateOut)
	}
	if err := recordpseudo.Verify(out, report.Produced); err != nil {
		t.Errorf("Verify refused the recorded cassette: %v", err)
	}
	file, err := cassette.LoadFile(path)
	if err != nil {
		t.Fatalf("load back: %v", err)
	}
	if file.PseudonymKeyFingerprint != key.Fingerprint() {
		t.Errorf("pseudonym_key_fingerprint = %q, want %q", file.PseudonymKeyFingerprint, key.Fingerprint())
	}
	if len(file.Scopes) != 1 || len(file.Scopes[0].Facts) != 2 {
		t.Fatalf("recorded %d scopes / facts %v, want 1 scope with 2 facts", len(file.Scopes), file.Scopes)
	}
	if got := file.Scopes[0].ScopeID; !pseudoAccountRe.MatchString(got) {
		t.Errorf("scope_id = %q, want the aws:<0000 account>:us-east-1:ecs shape", got)
	}
	if report.KeyFingerprint != key.Fingerprint() {
		t.Errorf("report fingerprint = %q, want %q", report.KeyFingerprint, key.Fingerprint())
	}
	if report.Facts != 2 || report.Scopes != 1 {
		t.Errorf("report facts/scopes = %d/%d, want 2/1", report.Facts, report.Scopes)
	}
	// runtime_id is deliberately unclassified in the aws policy: it must be
	// opaque and reported by path, never by value.
	if !containsString(report.OpaquePaths, "payload.attributes.containers[].runtime_id") {
		t.Errorf("opaque paths = %v, want payload.attributes.containers[].runtime_id reported", report.OpaquePaths)
	}
}

// TestRunRefusesWithoutKeyWhenRequired: RequirePseudonymization with no
// configured key must refuse before reading a single generation and write
// nothing.
func TestRunRefusesWithoutKeyWhenRequired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refused.json")
	src := &fakeSource{gens: []collector.CollectedGeneration{awsShapedGeneration()}}
	err := recorder.Run(context.Background(), src, recorder.Options{
		Path:                    path,
		CollectorLabel:          "aws",
		RequirePseudonymization: true,
	})
	if err == nil {
		t.Fatal("recorder.Run without a key = nil error, want refusal")
	}
	if !strings.Contains(err.Error(), "pseudonym") {
		t.Errorf("error %q does not name pseudonymization", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("recorder wrote a cassette although pseudonymization was required and unconfigured")
	}
	if src.index != 0 {
		t.Errorf("source was polled %d time(s) before the refusal, want 0", src.index)
	}
}

// TestRunWritesNothingWhenVerifyRefuses plants a policy that keeps the
// hosted-zone name verbatim (no other rule learns it, unlike the account,
// which every ARN also teaches): the raw zone survives canonicalization,
// Verify must refuse it, and no file may exist afterwards.
func TestRunWritesNothingWhenVerifyRefuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leak.json")
	leaky := recordpolicy.Policy()
	leaky.Fields["hosted_zone_name"] = recordpseudo.ClassKeep
	err := recorder.Run(context.Background(), &fakeSource{gens: []collector.CollectedGeneration{awsShapedGeneration()}}, recorder.Options{
		Path:           path,
		CollectorLabel: "aws",
		Pseudonymize:   &recordpseudo.Config{Key: testPseudonymKey(t), Policy: leaky},
	})
	if err == nil {
		t.Fatal("recorder.Run with a leaking policy = nil error, want Verify refusal")
	}
	if strings.Contains(err.Error(), rawZone) {
		t.Errorf("the refusal printed the raw zone")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("recorder wrote a cassette that Verify refused")
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
