// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Synthetic values only: the account is the private-data gate's own planted
// sample; names are invented.
const (
	recordAccount = "210987654321"
	recordRepo    = "orders-api"
	recordKey     = "6965-p3-record-test-key-0123456789abcdef0123456789abcdef0123456789"
)

func recordInstancesJSON(account string) string {
	return `[{"instance_id":"aws-record-1","collector_kind":"aws","mode":"continuous","enabled":true,"claims_enabled":true,` +
		`"configuration":{"target_scopes":[{"account_id":"` + account + `","allowed_regions":["us-east-1"],` +
		`"allowed_services":["iam","ecr"],"credentials":{"mode":"local_workload_identity"}}]}}]`
}

func recordGetenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func testInstruments(t *testing.T) *telemetry.Instruments {
	t.Helper()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return instruments
}

func TestParseArgsRecordMode(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-mode", "record", "-cassette-file", "/tmp/out.json"})
	if err != nil {
		t.Fatalf("parseArgs record: %v", err)
	}
	if opts.mode != launchModeRecord || opts.cassetteFile != "/tmp/out.json" {
		t.Fatalf("opts = %+v", opts)
	}
	if _, err := parseArgs([]string{"-mode", "record"}); err == nil {
		t.Error("record without -cassette-file accepted")
	}
	if _, err := parseArgs([]string{"-mode", "record", "-cassette-file", "/tmp/out.json", "-config", "x.json"}); err == nil {
		t.Error("record with -config accepted")
	}
}

// TestRecordPseudonymKeyIsReadOnceAndRequired: the key comes from
// ESHU_RECORD_PSEUDONYM_KEY, blank or short material is refused, and the
// loader never echoes the material into its error.
func TestRecordPseudonymKeyIsReadOnceAndRequired(t *testing.T) {
	t.Parallel()
	reads := 0
	getenv := func(key string) string {
		if key == envRecordPseudonymKey {
			reads++
			return recordKey
		}
		return ""
	}
	key, err := loadRecordPseudonymKey(getenv)
	if err != nil || key.IsZero() || reads != 1 {
		t.Fatalf("key err=%v zero=%v reads=%d", err, key.IsZero(), reads)
	}
	if _, err := loadRecordPseudonymKey(recordGetenv(nil)); err == nil || !strings.Contains(err.Error(), envRecordPseudonymKey) {
		t.Errorf("missing key: err=%v, want it to name the variable", err)
	}
	if _, err := loadRecordPseudonymKey(recordGetenv(map[string]string{envRecordPseudonymKey: "short-secret"})); err == nil || strings.Contains(err.Error(), "short-secret") {
		t.Errorf("short key: err=%v, want refusal without the material", err)
	}
}

// TestRecordSourceIsClaimedLiveWiringMinusStores: buildClaimedService is
// untouched by record mode. The record source must be exactly the live
// ClaimedSource with the three Postgres-backed collaborators (Limiter,
// Checkpoints, ScanStatus, and the scanner factory's Checkpoints) nil.
func TestRecordSourceIsClaimedLiveWiringMinusStores(t *testing.T) {
	t.Parallel()
	getenv := recordGetenv(map[string]string{"ESHU_COLLECTOR_INSTANCES_JSON": recordInstancesJSON("123456789012")})
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	tracer := noop.NewTracerProvider().Tracer("test")
	instruments := testInstruments(t)
	meter := sdkmetric.NewMeterProvider().Meter("test")
	live, err := buildClaimedService(postgres.SQLDB{}, getenv, tracer, instruments, nil, meter)
	if err != nil {
		t.Fatalf("buildClaimedService: %v", err)
	}
	liveSource, ok := live.Source.(awsruntime.ClaimedSource)
	if !ok {
		t.Fatalf("live source is %T", live.Source)
	}
	liveSource.Limiter = nil
	liveSource.Checkpoints = nil
	liveSource.ScanStatus = nil
	factory, ok := liveSource.Scanners.(awsruntime.DefaultScannerFactory)
	if !ok {
		t.Fatalf("live scanner factory is %T", liveSource.Scanners)
	}
	factory.Checkpoints = nil
	liveSource.Scanners = factory

	record := buildRecordSource(config, tracer, instruments)
	if !reflect.DeepEqual(record.Claimed, liveSource) {
		t.Fatalf("record source differs from the live wiring minus stores:\nrecord: %#v\nlive:   %#v", record.Claimed, liveSource)
	}
}

type fakeLease struct{}

func (fakeLease) Release() error { return nil }

type fakeCredentials struct{}

func (fakeCredentials) Acquire(context.Context, awsruntime.Target, time.Time) (awsruntime.CredentialLease, error) {
	return fakeLease{}, nil
}

// fakeScanners emits real-shaped envelopes through the production envelope
// builders for whichever service is claimed, with raw synthetic identifiers.
type fakeScanners struct{}

func (fakeScanners) Scanner(_ context.Context, target awsruntime.Target, boundary awscloud.Boundary, _ awsruntime.CredentialLease) (awsruntime.ServiceScanner, error) {
	return fakeScanner{target: target, boundary: boundary}, nil
}

type fakeScanner struct {
	target   awsruntime.Target
	boundary awscloud.Boundary
}

func (s fakeScanner) Scan(_ context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	switch s.target.ServiceKind {
	case awscloud.ServiceECR:
		repoARN := "arn:aws:ecr:us-east-1:" + recordAccount + ":repository/" + recordRepo
		resource, err := awscloud.NewResourceEnvelope(awscloud.ResourceObservation{
			Boundary: boundary, ARN: repoARN, ResourceID: repoARN, ResourceType: "ecr.repository", Name: recordRepo,
			Attributes: map[string]any{
				"uri":                  recordAccount + ".dkr.ecr.us-east-1.amazonaws.com/" + recordRepo,
				"image_tag_mutability": "IMMUTABLE",
			},
			Tags:      map[string]string{"Name": recordRepo, "acme-demo:owner": "team-orders"},
			SourceURI: "aws://ecr/" + recordAccount + "/us-east-1/" + recordRepo,
		})
		if err != nil {
			return nil, err
		}
		return []facts.Envelope{resource}, nil
	default:
		roleARN := "arn:aws:iam::" + recordAccount + ":role/orders-deployer"
		resource, err := awscloud.NewResourceEnvelope(awscloud.ResourceObservation{
			Boundary: boundary, ARN: roleARN, ResourceID: roleARN, ResourceType: "aws_iam_role", Name: "orders-deployer",
			Attributes: map[string]any{"path": "/service-role/"},
			SourceURI:  "aws://iam/" + recordAccount + "/role/orders-deployer",
		})
		if err != nil {
			return nil, err
		}
		policyARN := "arn:aws:iam::" + recordAccount + ":policy/orders-deploy-policy"
		relationship, err := awscloud.NewRelationshipEnvelope(awscloud.RelationshipObservation{
			Boundary: boundary, RelationshipType: "iam_role_attaches_policy", SourceResourceID: roleARN, SourceARN: roleARN,
			TargetResourceID: policyARN, TargetARN: policyARN, TargetType: "aws_iam_policy",
			SourceURI: "aws://iam/" + recordAccount + "/role/orders-deployer/policies",
		})
		if err != nil {
			return nil, err
		}
		return []facts.Envelope{resource, relationship}, nil
	}
}

// gateScan runs the committed private-data gate library over dir under a
// bash >= 4.3 and returns the exit code and combined output.
func gateScan(t *testing.T, dir string) (int, string) {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	lib := filepath.Join(repoRoot, "scripts", "lib", "cassette_private_data_pattern.sh")
	shell := ""
	for _, candidate := range []string{"/opt/homebrew/bin/bash", "/usr/local/bin/bash", "bash"} {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		if exec.Command(path, "-c", `[[ "${BASH_VERSINFO[0]}" -gt 4 || ("${BASH_VERSINFO[0]}" -eq 4 && "${BASH_VERSINFO[1]}" -ge 3) ]]`).Run() == nil {
			shell = path
			break
		}
	}
	if shell == "" {
		t.Skip("no bash >= 4.3 available for the private-data gate library")
	}
	script := `set -euo pipefail; fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }; source "$1"; cassette_private_data_scan "$2"`
	out, err := exec.Command(shell, "-c", script, "gate", lib, dir).CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), string(out)
	}
	t.Fatalf("run gate: %v", err)
	return -1, ""
}

// TestRecordCassetteIsGateCleanAndLogsNoValue drives the record path over a
// fake scanner factory: the written cassette passes the private-data gate,
// carries the key fingerprint, replays, and the collector.record.* log
// events carry counts and paths but no raw value and never the key.
func TestRecordCassetteIsGateCleanAndLogsNoValue(t *testing.T) {
	getenv := recordGetenv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": recordInstancesJSON(recordAccount),
		envRecordPseudonymKey:           recordKey,
	})
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	key, err := loadRecordPseudonymKey(getenv)
	if err != nil {
		t.Fatalf("loadRecordPseudonymKey: %v", err)
	}
	source := buildRecordSource(config, noop.NewTracerProvider().Tracer("test"), testInstruments(t))
	source.Claimed.Credentials = fakeCredentials{}
	source.Claimed.Scanners = fakeScanners{}
	source.Claimed.Clock = func() time.Time { return time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC) }

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	dir := t.TempDir()
	path := filepath.Join(dir, "recorded.json")
	if err := recordCassette(context.Background(), source, path, key, logger); err != nil {
		t.Fatalf("recordCassette: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{recordAccount, recordRepo, "orders-deployer", "acme-demo", "team-orders"} {
		if bytes.Contains(out, []byte(raw)) {
			t.Errorf("raw token of length %d reached the cassette", len(raw))
		}
	}
	if code, gateOut := gateScan(t, dir); code != 0 {
		t.Errorf("private-data gate exit %d:\n%s", code, gateOut)
	}
	file, err := cassette.LoadFile(path)
	if err != nil {
		t.Fatalf("load back: %v", err)
	}
	if file.PseudonymKeyFingerprint != key.Fingerprint() || file.Collector != "aws" {
		t.Errorf("file collector=%q fingerprint=%q", file.Collector, file.PseudonymKeyFingerprint)
	}
	if len(file.Scopes) != 2 {
		t.Errorf("scopes = %d, want 2 (iam, ecr)", len(file.Scopes))
	}

	logText := logs.String()
	for _, event := range []string{"collector.record.started", "collector.record.pseudonymized", "collector.record.completed"} {
		if !strings.Contains(logText, event) {
			t.Errorf("log event %s missing", event)
		}
	}
	for _, secret := range []string{recordKey, recordAccount, recordRepo, "orders-deployer", "acme-demo"} {
		if strings.Contains(logText, secret) {
			t.Errorf("log carries a value or the key (length %d)", len(secret))
		}
	}
	var pseudonymized map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logText), "\n") {
		if strings.Contains(line, "collector.record.pseudonymized") {
			if err := json.Unmarshal([]byte(line), &pseudonymized); err != nil {
				t.Fatalf("decode log line: %v", err)
			}
		}
	}
	if pseudonymized["key_fingerprint"] != key.Fingerprint() {
		t.Errorf("pseudonymized event fingerprint = %v", pseudonymized["key_fingerprint"])
	}
	for _, field := range []string{"scopes", "facts", "tokens", "opaque_paths", "unclassified_paths", "ipv4_collisions", "learned_by_class"} {
		if _, ok := pseudonymized[field]; !ok {
			t.Errorf("pseudonymized event lacks %s", field)
		}
	}
}
