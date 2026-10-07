// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// fillNonZero sets every settable part of v to a non-zero value, so blanking
// one RawSnapshot field later is always a visible change to a reader of it.
func fillNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(7)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillNonZero(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillNonZero(v.Index(0))
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		key, elem := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fillNonZero(key)
		fillNonZero(elem)
		v.SetMapIndex(key, elem)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)))
			return
		}
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				fillNonZero(v.Field(i))
			}
		}
	}
}

// scrapeDependsOn returns the RawSnapshot fields whose removal changes what a
// scrape renders or the health the report computes, with every other field
// populated. It is the measured read set of renderStatusMetrics and
// evaluateHealth, not a list written down by hand.
func scrapeDependsOn(t *testing.T) []string {
	t.Helper()
	full := statuspkg.RawSnapshot{}
	fillNonZero(reflect.ValueOf(&full).Elem())
	// A non-empty source would add the summary gauges; keep it out of the
	// comparison so only the sections are measured.
	full.ActiveWorkSource = statuspkg.ActiveWorkSource{}
	render := func(raw statuspkg.RawSnapshot) (string, statuspkg.HealthSummary) {
		report := statuspkg.BuildReport(raw, statuspkg.DefaultOptions())
		return renderStatusMetrics("svc", report), report.Health
	}
	wantBody, wantHealth := render(full)
	var depends []string
	rawType := reflect.TypeOf(full)
	for i := range rawType.NumField() {
		blanked := full
		field := reflect.ValueOf(&blanked).Elem().Field(i)
		field.Set(reflect.Zero(field.Type()))
		body, health := render(blanked)
		if body != wantBody || !reflect.DeepEqual(health, wantHealth) {
			depends = append(depends, rawType.Field(i).Name)
		}
	}
	slices.Sort(depends)
	return depends
}

// TestScrapeReadSetExcludesTheSectionsTheSelectionOmits measures, field by
// field, which RawSnapshot sections change the scrape's bytes or the computed
// health, and requires the sections the metrics selection omits (Terraform
// serials and warnings, collector fact evidence, registry collectors) to be
// outside that set. The seeded violation is built in: the same measurement must
// find every section the render does read, so a measurement that could not see a
// dependency fails on the non-vacuity list. Health reads ProducerActivity only
// behind earlier decisive checks (status/health.go), which the all-populated
// baseline always trips, so this measurement cannot see it and it is not on the
// list; TestScrapeBytesIgnorePopulatedOmittedSections measures health with only
// the omitted sections populated instead.
func TestScrapeReadSetExcludesTheSectionsTheSelectionOmits(t *testing.T) {
	t.Parallel()
	depends := scrapeDependsOn(t)
	t.Logf("RawSnapshot sections the scrape depends on: %s", strings.Join(depends, ", "))

	for _, read := range []string{
		"ScopeActivity", "GenerationCounts", "StageCounts", "DomainBacklogs", "Queue",
		"CollectorGenerationDeadLetters", "Coordinator", "RetryPolicies",
	} {
		if !slices.Contains(depends, read) {
			t.Fatalf("the measurement did not see %s, a section the scrape reads: %v", read, depends)
		}
	}
	selection := metricsSnapshotSelection()
	omitted := map[string]bool{
		"TerraformStateLastSerials":    selection.SkipTerraformStateEvidence,
		"TerraformStateRecentWarnings": selection.SkipTerraformStateEvidence,
		"CollectorFactEvidence":        !selection.IncludeCollectorFactEvidence,
		"RegistryCollectors":           !selection.IncludeRegistryCollectors,
	}
	for field, isOmitted := range omitted {
		if !isOmitted {
			t.Fatalf("the metrics selection still requests %s", field)
		}
		if slices.Contains(depends, field) {
			t.Fatalf("the scrape reads %s but the metrics selection omits it", field)
		}
	}
}

// TestScrapeBytesIgnorePopulatedOmittedSections renders the scrape from a
// populated snapshot with and without the omitted sections and requires
// identical bytes and identical health, and plants a violation: removing a
// section the render does read must change the bytes.
func TestScrapeBytesIgnorePopulatedOmittedSections(t *testing.T) {
	t.Parallel()
	populated := terraformMetricsSnapshot()
	populated.RegistryCollectors = []statuspkg.RegistryCollectorSnapshot{
		{CollectorKind: "oci_registry", ConfiguredInstances: 2, ActiveScopes: 3, RetryableFailures: 4, TerminalFailures: 5},
	}
	render := func(raw statuspkg.RawSnapshot) (string, statuspkg.HealthSummary) {
		report := statuspkg.BuildReport(raw, statuspkg.DefaultOptions())
		return renderStatusMetrics("collector-git", report), report.Health
	}
	wantBody, wantHealth := render(populated)

	narrowed := populated
	narrowed.CollectorFactEvidence, narrowed.RegistryCollectors = nil, nil
	narrowed.TerraformStateLastSerials, narrowed.TerraformStateRecentWarnings = nil, nil
	body, health := render(narrowed)
	if body != wantBody || !reflect.DeepEqual(health, wantHealth) {
		t.Fatalf("omitting the unrendered sections changed the scrape:\nfull=%s\nnarrowed=%s", wantBody, body)
	}

	// Health on its own: only the omitted sections populated, everything else
	// empty, must be the health of an empty snapshot.
	onlyOmitted := statuspkg.RawSnapshot{
		CollectorFactEvidence: populated.CollectorFactEvidence, RegistryCollectors: populated.RegistryCollectors,
		TerraformStateLastSerials: populated.TerraformStateLastSerials, TerraformStateRecentWarnings: populated.TerraformStateRecentWarnings,
	}
	if got, want := statuspkg.BuildReport(onlyOmitted, statuspkg.DefaultOptions()).Health, statuspkg.BuildReport(statuspkg.RawSnapshot{}, statuspkg.DefaultOptions()).Health; !reflect.DeepEqual(got, want) {
		t.Fatalf("the omitted sections changed health: %+v, want %+v", got, want)
	}

	violation := populated
	violation.Queue = statuspkg.QueueSnapshot{}
	if violationBody, _ := render(violation); violationBody == wantBody {
		t.Fatal("removing the queue, which the scrape renders, did not change the bytes: the comparison cannot fail")
	}
}
