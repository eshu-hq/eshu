// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildReportCarriesTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	source := ActiveWorkSource{
		Source: ActiveWorkSourceModel, Reason: ActiveWorkReasonFresh, AsOf: asOf.Add(-9 * time.Second), Age: 9 * time.Second,
	}
	report := BuildReport(RawSnapshot{AsOf: asOf, ActiveWorkSource: source}, DefaultOptions())
	if report.ActiveWorkSource != source {
		t.Fatalf("report.ActiveWorkSource = %+v, want %+v", report.ActiveWorkSource, source)
	}
}

func TestActiveWorkSourceJSONNamesTheContract(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	view := ActiveWorkSource{
		Source: ActiveWorkSourceLiveFallback, Reason: ActiveWorkReasonStale, AsOf: asOf, Age: 0,
	}.JSON()
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"source":"live_fallback","reason":"stale","as_of":"2026-10-06T12:00:00Z","age_seconds":0,"stale":false}`
	if string(encoded) != want {
		t.Fatalf("json = %s, want %s", encoded, want)
	}
}

func TestActiveWorkSourceIsOmittedWhenTheReaderReportsNone(t *testing.T) {
	t.Parallel()

	if got := (ActiveWorkSource{}).JSON(); got != nil {
		t.Fatalf("zero source JSON = %+v, want nil so a reader that does not report one adds no key", got)
	}
	payload, err := RenderJSON(BuildReport(RawSnapshot{}, DefaultOptions()))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, present := decoded["active_work_source"]; present {
		t.Fatal("RenderJSON emitted active_work_source for a snapshot with no source")
	}
}

func TestRenderJSONEmitsTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	raw := RawSnapshot{AsOf: asOf, ActiveWorkSource: ActiveWorkSource{
		Source: ActiveWorkSourceModel, Reason: ActiveWorkReasonFresh, AsOf: asOf.Add(-4500 * time.Millisecond), Age: 4500 * time.Millisecond,
	}}
	payload, err := RenderJSON(BuildReport(raw, DefaultOptions()))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Source *struct {
			Source string  `json:"source"`
			Reason string  `json:"reason"`
			AsOf   string  `json:"as_of"`
			Age    float64 `json:"age_seconds"`
			Stale  bool    `json:"stale"`
		} `json:"active_work_source"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Source == nil {
		t.Fatalf("active_work_source missing from %s", payload)
	}
	if got := *decoded.Source; got.Source != "model" || got.Reason != "fresh" || got.Age != 4.5 || got.Stale || got.AsOf != "2026-10-06T11:59:55.5Z" {
		t.Fatalf("active_work_source = %+v", got)
	}
}

func TestBuildReportCarriesTheTerraformStateSource(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)
	source := ActiveWorkSource{
		Source: ActiveWorkSourceModel, Reason: ActiveWorkReasonFresh, AsOf: asOf.Add(-12 * time.Second), Age: 12 * time.Second,
	}
	report := BuildReport(RawSnapshot{AsOf: asOf, TerraformStateSource: source}, DefaultOptions())
	if report.TerraformStateSource != source {
		t.Fatalf("report.TerraformStateSource = %+v, want %+v", report.TerraformStateSource, source)
	}
	if report.ActiveWorkSource != (ActiveWorkSource{}) {
		t.Fatalf("report.ActiveWorkSource = %+v, want it untouched by the Terraform-state marker", report.ActiveWorkSource)
	}
}

// TestRenderJSONEmitsTheTerraformStateSourceBesideTheSection: the marker is a
// sibling of terraform_state, so it is present even when the section is empty
// and omitted from the payload (an empty section renders no key).
func TestRenderJSONEmitsTheTerraformStateSourceBesideTheSection(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)
	raw := RawSnapshot{AsOf: asOf, TerraformStateSource: ActiveWorkSource{
		Source: ActiveWorkSourceLiveFallback, Reason: ActiveWorkReasonStale, AsOf: asOf,
	}}
	payload, err := RenderJSON(BuildReport(raw, DefaultOptions()))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, present := decoded["terraform_state"]; present {
		t.Fatal("an empty Terraform-state section rendered a key; the marker must not depend on it")
	}
	var source ActiveWorkSourceJSON
	if err := json.Unmarshal(decoded["terraform_state_source"], &source); err != nil {
		t.Fatalf("terraform_state_source: %v in %s", err, payload)
	}
	if source.Source != "live_fallback" || source.Reason != "stale" || source.AsOf != "2026-10-07T09:30:00Z" || source.Stale {
		t.Fatalf("terraform_state_source = %+v", source)
	}
	if _, present := decoded["active_work_source"]; present {
		t.Fatal("RenderJSON emitted active_work_source for a snapshot with none")
	}
}

func TestRenderJSONOmitsTheTerraformStateSourceWhenTheReaderReportsNone(t *testing.T) {
	t.Parallel()

	payload, err := RenderJSON(BuildReport(RawSnapshot{}, DefaultOptions()))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, present := decoded["terraform_state_source"]; present {
		t.Fatal("RenderJSON emitted terraform_state_source for a snapshot with no source")
	}
}
