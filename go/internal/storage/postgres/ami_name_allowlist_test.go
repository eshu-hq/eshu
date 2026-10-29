// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// amiNameBenignFixtures are real-shaped AMI names the hybrid ruling on #9525
// admits: distro/version labels for CVE triage and golden-image identity that
// typed fields plus ami_id cannot serve. Every one fits the AWS CreateImage
// structural contract (3-128 runes, alphanumerics plus ()[] ./-'@_), so the
// loader must surface each verbatim.
var amiNameBenignFixtures = []string{
	"ubuntu-jammy-22.04-amd64-server-20230601",
	"RHEL-9.3.0_HVM-20231113-x86_64",
	"amzn2-ami-kernel-5.10-hvm-2.0.20231116.0-x86_64-gp2",
	"Windows_Server-2022-English-Full-Base-2023.11.15",
	"golden-image (prod) v1.2",
}

// amiSourcePayload builds one aws_resource source-fact payload for an AMI
// whose attributes map carries the given raw value under key "name" plus the
// always-dropped description sentinel, mirroring stored/replayed JSONB bytes
// including a hand-forged rogue producer payload.
func amiSourcePayload(t *testing.T, nameValue any) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"arn":           "",
		"resource_type": "aws_ec2_ami",
		"account_id":    "111122223333",
		"resource_id":   "ami-0000000000000000a",
		"region":        "us-east-1",
		"attributes": map[string]any{
			"name":        nameValue,
			"description": "must-not-surface",
		},
	})
	if err != nil {
		t.Fatalf("marshal AMI source payload: %v", err)
	}
	return payload
}

// loadAMILoaderRecord runs the real loader over one stored AMI source-fact row
// and returns its admitted record: payload bytes to record, the
// stored/replayed leg of the #9525 proof battery.
func loadAMILoaderRecord(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{cloud.AWSResourceFactKind, "ami-0000000000000000a", payload},
			}},
		},
	}
	loader := PostgresCloudInventoryEvidenceLoader{DB: db}
	records, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	record := records[0]
	if record.ResourceType != "aws_ec2_ami" || record.AccountID != "111122223333" {
		t.Fatalf("record identity = %#v, want aws_ec2_ami in 111122223333", record)
	}
	return record.Attributes
}

// TestCloudInventoryLoaderAdmitsBenignAMIName is the benign-present leg: every
// real-shaped name passes stored bytes to loader verbatim, and the rogue
// description key never rides along even from a hand-forged payload.
func TestCloudInventoryLoaderAdmitsBenignAMIName(t *testing.T) {
	t.Parallel()

	for _, name := range amiNameBenignFixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			attrs := loadAMILoaderRecord(t, amiSourcePayload(t, name))
			if got := attrs["name"]; got != name {
				t.Fatalf("attributes[name] = %#v, want verbatim %q", got, name)
			}
			if _, present := attrs["description"]; present {
				t.Fatalf("attributes[description] surfaced: %#v", attrs)
			}
		})
	}
}

// TestCloudInventoryLoaderDropsKeyValueAMIName proves a key=value-shaped name
// is dropped at the loader while the typed record survives: the charset has
// no "=" and the hygiene scan trips on the sensitive key, so either gate
// alone drops it. The exactly-one-deduped-warning leg lives producer-side
// (satellite lane owns aws_warning emission; this loader is read-only).
func TestCloudInventoryLoaderDropsKeyValueAMIName(t *testing.T) {
	t.Parallel()

	attrs := loadAMILoaderRecord(t, amiSourcePayload(t, "api_key=sk-live-synthetic-value"))
	if _, present := attrs["name"]; present {
		t.Fatalf("key=value attributes[name] surfaced: %#v", attrs)
	}
	if _, present := attrs["description"]; present {
		t.Fatalf("attributes[description] surfaced: %#v", attrs)
	}
}

// TestCloudInventoryLoaderDropsNonconformingAMIName is the structural
// fail-closed table: over-cap, charset-violating, control-bearing, non-string,
// blank, and too-short names are dropped with the typed record intact. Never
// truncated: a dropped name leaves no prefix behind.
func TestCloudInventoryLoaderDropsNonconformingAMIName(t *testing.T) {
	t.Parallel()

	cases := map[string]any{
		"over_cap_129":      strings.Repeat("a", 129),
		"colon":             "ami:name",
		"percent_encoded":   "ami%20name",
		"non_ascii":         "ubüntu-22.04",
		"newline":           "ab\ncd",
		"nul":               "ab\x00cd",
		"tab":               "ab\tcd",
		"non_string_number": float64(42),
		"non_string_bool":   true,
		"non_string_object": map[string]any{"nested": "nope"},
		"non_string_array":  []any{"a"},
		"blank":             "",
		"whitespace":        "   ",
		"one_rune":          "a",
		"two_runes":         "ab",
		// The #9450 sentinel stays dropped as a name value even though it is
		// charset-valid: exact-sentinel preservation, not secret detection.
		"sentinel": "must-not-surface",
	}
	for leg, value := range cases {
		leg, value := leg, value
		t.Run(leg, func(t *testing.T) {
			t.Parallel()
			attrs := loadAMILoaderRecord(t, amiSourcePayload(t, value))
			if _, present := attrs["name"]; present {
				t.Fatalf("attributes[name] = %#v, want dropped for %s", attrs["name"], leg)
			}
		})
	}
}

// TestCloudInventoryLoaderTrimsSurroundingWhitespaceAMIName pins the
// TrimSpace-before-admit behavior documented in cloud-inventory.md: padding
// is trimmed before validation, so the trimmed value is what is admitted and
// surfaced (never the padded original), and padding that trims below 3 runes
// still drops with the typed record intact.
func TestCloudInventoryLoaderTrimsSurroundingWhitespaceAMIName(t *testing.T) {
	t.Parallel()

	attrs := loadAMILoaderRecord(t, amiSourcePayload(t, "  abc  "))
	if got := attrs["name"]; got != "abc" {
		t.Fatalf("attributes[name] = %#v, want trimmed %q", got, "abc")
	}
	padded := loadAMILoaderRecord(t, amiSourcePayload(t, "  abc"))
	if got := padded["name"]; got != "abc" {
		t.Fatalf("attributes[name] = %#v, want trimmed %q", got, "abc")
	}
	short := loadAMILoaderRecord(t, amiSourcePayload(t, " a "))
	if _, present := short["name"]; present {
		t.Fatalf("attributes[name] = %#v, want dropped (trims to one rune)", short["name"])
	}
}

// TestCloudInventoryLoaderAdmitsAMINameBoundaries pins the exact AWS
// CreateImage bounds: 3 and 128 runes pass verbatim.
func TestCloudInventoryLoaderAdmitsAMINameBoundaries(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"abc", strings.Repeat("b", 128)} {
		name := name
		t.Run(name[:3]+"...", func(t *testing.T) {
			t.Parallel()
			attrs := loadAMILoaderRecord(t, amiSourcePayload(t, name))
			if got := attrs["name"]; got != name {
				t.Fatalf("attributes[name] = %#v, want verbatim %d-rune boundary", got, len([]rune(name)))
			}
		})
	}
}

// TestCloudInventoryLoaderAdmitsBareTokenAMIName pins the honesty clause of
// the #9525 ruling: a patternless bare token that fits the charset is
// EXPECTED TO PASS. The admitted claim is "AWS-constrained creator label,
// structurally validated", explicitly not "secret-free".
func TestCloudInventoryLoaderAdmitsBareTokenAMIName(t *testing.T) {
	t.Parallel()

	const bareToken = "xk9Mq2Vlx7Pr4Tw8Zna1"
	attrs := loadAMILoaderRecord(t, amiSourcePayload(t, bareToken))
	if got := attrs["name"]; got != bareToken {
		t.Fatalf("attributes[name] = %#v, want documented expected-pass %q", got, bareToken)
	}
}

// TestCloudInventoryAMINameAllowlistIsNarrow proves the admission is scoped to
// resourceType aws_ec2_ami only: the same name key is dropped for any other
// resource type, and the AMI entry admits nothing from the base AWS
// image/version allowlist.
func TestCloudInventoryAMINameAllowlistIsNarrow(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(map[string]any{
		"arn":           "arn:aws:ecs:us-east-1:111122223333:task/demo/aaa",
		"resource_type": "aws_ecs_task",
		"account_id":    "111122223333",
		"resource_id":   "demo",
		"region":        "us-east-1",
		"attributes": map[string]any{
			"name":        "ubuntu-jammy-22.04-amd64-server-20230601",
			"description": "must-not-surface",
		},
	})
	if err != nil {
		t.Fatalf("marshal ECS payload: %v", err)
	}
	record, ok := cloudInventoryRecordFromRow(cloud.AWSResourceFactKind, "arn:aws:ecs:us-east-1:111122223333:task/demo/aaa", payload)
	if !ok {
		t.Fatal("cloudInventoryRecordFromRow() ok = false, want true")
	}
	if _, present := record.Attributes["name"]; present {
		t.Fatalf("non-AMI attributes[name] surfaced: %#v", record.Attributes)
	}

	amiAttrs := loadAMILoaderRecord(t, func() []byte {
		raw, err := json.Marshal(map[string]any{
			"arn":           "",
			"resource_type": "aws_ec2_ami",
			"account_id":    "111122223333",
			"resource_id":   "ami-0000000000000000a",
			"region":        "us-east-1",
			"attributes": map[string]any{
				"name":                "ubuntu-jammy-22.04-amd64-server-20230601",
				"task_definition_arn": "arn:aws:ecs:us-east-1:111122223333:task-definition/demo:1",
			},
		})
		if err != nil {
			t.Fatalf("marshal AMI payload: %v", err)
		}
		return raw
	}())
	if _, present := amiAttrs["task_definition_arn"]; present {
		t.Fatalf("AMI entry leaked base-allowlist key: %#v", amiAttrs)
	}
}

// TestCloudInventoryLoaderNeverLogsAMINameText is the diagnostics-marker-only
// leg: neither the admit path nor the skip diagnostic ever carries the
// creator-controlled name or description text. The skip row below proves the
// redaction holds even when the loader logs.
func TestCloudInventoryLoaderNeverLogsAMINameText(t *testing.T) {
	t.Parallel()

	const name = "ubuntu-jammy-22.04-amd64-server-20230601"
	var logs bytes.Buffer
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{cloud.AWSResourceFactKind, "ami-0000000000000000a", amiSourcePayload(t, name)},
				{cloud.AWSResourceFactKind, "ami-0000000000000000b", []byte(`{"undecodable":`)},
			}},
		},
	}
	loader := PostgresCloudInventoryEvidenceLoader{
		DB:     db,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	records, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1 (undecodable row skipped)", len(records))
	}
	for _, secret := range []string{name, "must-not-surface"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("loader logs carry creator text %q: %s", secret, logs.String())
		}
	}
}
