// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
	scannerworkerv1 "github.com/eshu-hq/eshu/sdk/go/factschema/scannerworker/v1"
)

// warningScopeKeys are the two optional keys core eshu-hq/eshu#10043 adds to
// scanner_worker.warning so a failed Go reachability scan can be joined to a
// repository and to the go.mod file that failed.
var warningScopeKeys = []string{"repository_id", "relative_path"}

// warningCoreRequired is the required set the warning already carries; the
// change must not widen it.
var warningCoreRequired = []string{
	"analysis_status", "analyzer", "coverage_status", "reason",
	"target_kind", "target_locator_hash", "warning_class",
}

func scannerWorkerWarningCorePayload() map[string]any {
	return map[string]any{
		"analyzer":            "go_reachability",
		"target_kind":         "repository",
		"target_locator_hash": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"reason":              "toolchain_unsupported",
		"warning_class":       "scanner_worker_warning",
		"analysis_status":     "failed",
		"coverage_status":     "unscanned",
	}
}

// TestDecodeScannerWorkerWarningScopeFromLiteralJSON decodes a literal JSON
// document, as a producer emits it, and reads each struct field. A
// decode-then-encode round trip cancels a swap between two json tags, so only
// reading the fields sees a key bound to the wrong field.
func TestDecodeScannerWorkerWarningScopeFromLiteralJSON(t *testing.T) {
	const document = `{
		"analyzer": "go_reachability",
		"target_kind": "repository",
		"target_locator_hash": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"reason": "toolchain_unsupported",
		"warning_class": "scanner_worker_warning",
		"analysis_status": "failed",
		"coverage_status": "unscanned",
		"repository_id": "repository:r_0a1b2c3d",
		"relative_path": "tools/go.mod"
	}`
	var payload map[string]any
	if err := json.Unmarshal([]byte(document), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	warning, err := DecodeScannerWorkerWarning(testScannerWorkerEnvelope(FactKindScannerWorkerWarning, payload))
	if err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v", err)
	}
	if warning.RepositoryID == nil || *warning.RepositoryID != "repository:r_0a1b2c3d" {
		t.Fatalf("RepositoryID = %v, want repository:r_0a1b2c3d", warning.RepositoryID)
	}
	if warning.RelativePath == nil || *warning.RelativePath != "tools/go.mod" {
		t.Fatalf("RelativePath = %v, want tools/go.mod", warning.RelativePath)
	}
}

// TestDecodeScannerWorkerWarningWithoutScopeIsStillValid proves the change is
// additive: a warning written before the fields existed, including the
// repository-wide failure that names no go.mod, decodes with nil scope.
func TestDecodeScannerWorkerWarningWithoutScopeIsStillValid(t *testing.T) {
	warning, err := DecodeScannerWorkerWarning(
		testScannerWorkerEnvelope(FactKindScannerWorkerWarning, scannerWorkerWarningCorePayload()))
	if err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v", err)
	}
	if warning.RepositoryID != nil || warning.RelativePath != nil {
		t.Fatalf("scope = (%v, %v), want both nil for an older payload", warning.RepositoryID, warning.RelativePath)
	}

	// A repository-wide failure carries repository_id and no relative_path.
	payload := scannerWorkerWarningCorePayload()
	payload["repository_id"] = "repository:r_0a1b2c3d"
	warning, err = DecodeScannerWorkerWarning(testScannerWorkerEnvelope(FactKindScannerWorkerWarning, payload))
	if err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v", err)
	}
	if warning.RepositoryID == nil || warning.RelativePath != nil {
		t.Fatalf("scope = (%v, %v), want repository_id set and relative_path nil", warning.RepositoryID, warning.RelativePath)
	}
}

// TestDecodeScannerWorkerWarningScopeWrongTypeFailsClosed proves a scope field
// of the wrong JSON type is a classified input_invalid failure, not a silently
// dropped scan target.
func TestDecodeScannerWorkerWarningScopeWrongTypeFailsClosed(t *testing.T) {
	for _, key := range warningScopeKeys {
		for name, value := range map[string]any{
			"number": float64(7),
			"list":   []any{"x"},
			"object": map[string]any{"id": "x"},
			"bool":   true,
		} {
			t.Run(key+" is a "+name, func(t *testing.T) {
				payload := scannerWorkerWarningCorePayload()
				payload[key] = value
				_, err := DecodeScannerWorkerWarning(testScannerWorkerEnvelope(FactKindScannerWorkerWarning, payload))
				var classified *DecodeError
				if !errors.As(err, &classified) {
					t.Fatalf("error = %v (%T), want *DecodeError", err, err)
				}
				if classified.Classification != ClassificationInputInvalid {
					t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
				}
			})
		}
	}
}

// TestEncodeScannerWorkerWarningScopeRoundTrips proves the encoder writes no
// scope key for a warning with none, so an older golden stays byte stable, and
// that both keys survive decode, encode, decode.
func TestEncodeScannerWorkerWarningScopeRoundTrips(t *testing.T) {
	bare, err := DecodeScannerWorkerWarning(
		testScannerWorkerEnvelope(FactKindScannerWorkerWarning, scannerWorkerWarningCorePayload()))
	if err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v", err)
	}
	encoded, err := EncodeScannerWorkerWarning(bare)
	if err != nil {
		t.Fatalf("EncodeScannerWorkerWarning() error = %v", err)
	}
	for _, key := range warningScopeKeys {
		if _, ok := encoded[key]; ok {
			t.Fatalf("encoded payload carries %q for a warning with none: %v", key, encoded)
		}
	}

	full := scannerWorkerWarningCorePayload()
	full["repository_id"] = "repository:r_0a1b2c3d"
	full["relative_path"] = "tools/go.mod"
	decoded, err := DecodeScannerWorkerWarning(testScannerWorkerEnvelope(FactKindScannerWorkerWarning, full))
	if err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v", err)
	}
	again, err := EncodeScannerWorkerWarning(decoded)
	if err != nil {
		t.Fatalf("EncodeScannerWorkerWarning() error = %v", err)
	}
	for _, key := range warningScopeKeys {
		if got := again[key]; got != full[key] {
			t.Fatalf("round-tripped %s = %#v, want %#v", key, got, full[key])
		}
	}
}

// TestScannerWorkerWarningSchemaDeclaresScopeFields proves the generated
// schema, in both the canonical copy and the embedded fixture-pack copy,
// declares each scope field as an optional nullable string outside the required
// set, leaves the required set unchanged, and does not declare module_path.
func TestScannerWorkerWarningSchemaDeclaresScopeFields(t *testing.T) {
	canonical, err := os.ReadFile("schema/scanner_worker.warning.v1.schema.json")
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	packed, ok := fixturepack.SchemaFor("scanner_worker.warning")
	if !ok {
		t.Fatal("fixturepack has no scanner_worker.warning schema")
	}
	for name, raw := range map[string][]byte{"canonical": canonical, "fixturepack": packed} {
		t.Run(name, func(t *testing.T) {
			var schema struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			required := append([]string(nil), schema.Required...)
			sort.Strings(required)
			if got, want := strings.Join(required, ","), strings.Join(warningCoreRequired, ","); got != want {
				t.Fatalf("required = %s, want %s", got, want)
			}
			if _, present := schema.Properties["module_path"]; present {
				t.Fatal("schema declares module_path; the warning joins on repository_id and relative_path, and a module line is evidence, not identity")
			}
			for _, key := range warningScopeKeys {
				field, present := schema.Properties[key]
				if !present {
					t.Fatalf("schema properties lack %q", key)
				}
				var shape struct {
					Type []string `json:"type"`
				}
				if err := json.Unmarshal(field, &shape); err != nil {
					t.Fatalf("json.Unmarshal(%s) error = %v", key, err)
				}
				if len(shape.Type) != 2 || shape.Type[0] != "string" || shape.Type[1] != "null" {
					t.Fatalf("%s schema = %s, want a nullable string", key, field)
				}
			}
		})
	}
}

// TestScannerWorkerWarningFixturePackCarriesScope proves the fixture pack's
// valid payload carries both scope keys, so a conformance run validates them
// against the schema, and its invalid payload still fails on the required set.
func TestScannerWorkerWarningFixturePackCarriesScope(t *testing.T) {
	payload, ok := fixturepack.ValidPayload("scanner_worker.warning")
	if !ok {
		t.Fatal("fixturepack has no valid scanner_worker.warning payload")
	}
	if _, stray := payload["module_path"]; stray {
		t.Fatal("valid fixture carries module_path; the schema allows extra keys, so only this check keeps it out")
	}
	if got := payload["warning_class"]; got != "scanner_worker_warning" {
		t.Fatalf("fixture warning_class = %v, want the constant scanner_worker_warning", got)
	}
	if payload["reason"] == payload["warning_class"] {
		t.Fatal("fixture reason equals warning_class; the failure class belongs in reason")
	}
	warning, err := DecodeScannerWorkerWarning(testScannerWorkerEnvelope(FactKindScannerWorkerWarning, payload))
	if err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v", err)
	}
	if warning.RepositoryID == nil || *warning.RepositoryID != "repository:r_0a1b2c3d" {
		t.Fatalf("RepositoryID = %v, want the fixture repository id", warning.RepositoryID)
	}
	if warning.RelativePath == nil || *warning.RelativePath != "tools/go.mod" {
		t.Fatalf("RelativePath = %v, want the fixture relative path", warning.RelativePath)
	}

	invalid, ok := fixturepack.InvalidPayload("scanner_worker.warning")
	if !ok {
		t.Fatal("fixturepack has no invalid scanner_worker.warning payload")
	}
	if _, err := DecodeScannerWorkerWarning(testScannerWorkerEnvelope(FactKindScannerWorkerWarning, invalid)); err == nil {
		t.Fatal("invalid fixture decoded, want a required-field failure")
	}
}

// TestEncodeScannerWorkerWarningWritesEveryField sets every field of the
// Warning struct and requires the hand-written encoder to emit one key per json
// tag and to round-trip the struct. A field added to the struct without an
// addOptionalString line in EncodeScannerWorkerWarning fails here, which the
// per-field tests above would not catch.
func TestEncodeScannerWorkerWarningWritesEveryField(t *testing.T) {
	var warning scannerworkerv1.Warning
	value := reflect.ValueOf(&warning).Elem()
	wantKeys := make([]string, 0, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		key := strings.Split(field.Tag.Get("json"), ",")[0]
		if key == "" || key == "-" {
			t.Fatalf("field %s has no json key", field.Name)
		}
		wantKeys = append(wantKeys, key)
		text := "value-for-" + key
		switch field.Type.Kind() {
		case reflect.String:
			value.Field(i).SetString(text)
		case reflect.Pointer:
			if field.Type.Elem().Kind() != reflect.String {
				t.Fatalf("field %s is a pointer to %s, want *string", field.Name, field.Type.Elem())
			}
			value.Field(i).Set(reflect.ValueOf(&text))
		default:
			t.Fatalf("field %s has unsupported kind %s", field.Name, field.Type.Kind())
		}
	}
	sort.Strings(wantKeys)

	encoded, err := EncodeScannerWorkerWarning(warning)
	if err != nil {
		t.Fatalf("EncodeScannerWorkerWarning() error = %v", err)
	}
	gotKeys := make([]string, 0, len(encoded))
	for key := range encoded {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("encoded keys = %v, want one per struct field %v", gotKeys, wantKeys)
	}

	decoded, err := DecodeScannerWorkerWarning(testScannerWorkerEnvelope(FactKindScannerWorkerWarning, encoded))
	if err != nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, warning) {
		t.Fatalf("round trip = %+v, want %+v", decoded, warning)
	}
}
