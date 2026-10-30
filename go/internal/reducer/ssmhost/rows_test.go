// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssmhost

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

var ssmRowsNow = time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC)

var ssmRowsBounds = Bounds{
	MaxCaptureAge:             2 * time.Hour,
	MaxCrossTypeSkew:          10 * time.Minute,
	MaxPlatformObservationAge: 24 * time.Hour,
}

func ssmAppPayload(instance, capture, name string) string {
	return `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"` + instance + `","resource_type":"AWS::EC2::Instance","source_type":"AWS:Application","schema_version":"1.0.0","capture_time":"` + capture + `","name":"` + name + `","version":"1.0"}`
}

func ssmPlatformPayload(instance, source, capture, observed string) string {
	captureField := ""
	if capture != "" {
		captureField = `"capture_time":"` + capture + `",`
	}
	return `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"` + instance + `","resource_type":"AWS::EC2::Instance","platform_type":"Linux","platform_name":"ExampleLinux","source_type":"` + source + `",` + captureField + `"observation_time":"` + observed + `"}`
}

func ssmFact(kind, id, payload string, observed time.Time) facts.Envelope {
	env := ssmEnvelope(kind, payload)
	env.FactID = id
	env.ObservedAt = observed
	return env
}

func TestProjectHostsPositiveSupersedes(t *testing.T) {
	apps := []facts.Envelope{
		ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
		ssmFact("aws_ssm_host_application", "a2", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "beta"), ssmRowsNow),
	}
	plats := []facts.Envelope{
		ssmFact("aws_ssm_host_platform", "p1", ssmPlatformPayload("i-1", "ssm_inventory", "2026-10-29T22:31:19Z", "2026-10-29T22:33:02Z"), ssmRowsNow),
	}
	observations, platformObservations, quarantined, err := ExtractObservations(apps, plats)
	if err != nil {
		t.Fatalf("ExtractObservations: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0", len(quarantined))
	}
	projections, tally := ProjectHosts(observations, platformObservations, nil, ssmRowsBounds, ssmRowsNow)
	if len(projections) != 1 {
		t.Fatalf("projections = %d, want 1 host", len(projections))
	}
	got := projections[0]
	if !got.Supersede {
		t.Fatalf("Supersede = false, reason %q, want true", got.Reason)
	}
	if len(got.AppRows) != 2 {
		t.Fatalf("app rows = %d, want 2", len(got.AppRows))
	}
	if got.AppRows[0]["uid"] == got.AppRows[1]["uid"] {
		t.Fatalf("app row uids collide")
	}
	if len(got.PlatformRows) != 1 {
		t.Fatalf("platform rows = %d, want 1", len(got.PlatformRows))
	}
	if len(got.EdgeRows) != 2 {
		t.Fatalf("edges = %d, want 2 (one per app row to the paired platform)", len(got.EdgeRows))
	}
	for _, edge := range got.EdgeRows {
		if edge["relationship"] != "REPORTED_WITH" {
			t.Fatalf("edge relationship = %v, want REPORTED_WITH", edge["relationship"])
		}
	}
	if tally.Superseded != 1 {
		t.Fatalf("tally.Superseded = %d, want 1", tally.Superseded)
	}
}

func TestProjectHostsDuplicateDeliveryDeduplicates(t *testing.T) {
	payload := ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha")
	apps := []facts.Envelope{
		ssmFact("aws_ssm_host_application", "a1", payload, ssmRowsNow.Add(-time.Minute)),
		ssmFact("aws_ssm_host_application", "a2", payload, ssmRowsNow),
	}
	observations, _, _, err := ExtractObservations(apps, nil)
	if err != nil {
		t.Fatalf("ExtractObservations: %v", err)
	}
	projections, _ := ProjectHosts(observations, nil, nil, ssmRowsBounds, ssmRowsNow)
	if len(projections) != 1 || len(projections[0].AppRows) != 1 {
		t.Fatalf("duplicate delivery produced %d projections / %d rows, want 1/1", len(projections), len(projections[0].AppRows))
	}
	if got := projections[0].AppRows[0]["source_fact_id"]; got != "a2" {
		t.Fatalf("duplicate winner source_fact_id = %v, want a2 (max source order wins)", got)
	}
}

func TestProjectHostsMixedCapturesPreserve(t *testing.T) {
	apps := []facts.Envelope{
		ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
		ssmFact("aws_ssm_host_application", "a2", ssmAppPayload("i-1", "2026-10-29T22:35:44Z", "beta"), ssmRowsNow),
	}
	observations, _, _, err := ExtractObservations(apps, nil)
	if err != nil {
		t.Fatalf("ExtractObservations: %v", err)
	}
	projections, tally := ProjectHosts(observations, nil, nil, ssmRowsBounds, ssmRowsNow)
	if len(projections) != 1 {
		t.Fatalf("projections = %d, want 1", len(projections))
	}
	if projections[0].Supersede {
		t.Fatalf("Supersede = true on mixed captures, want false (preserve prior, never pick a winner)")
	}
	if len(projections[0].AppRows) != 0 {
		t.Fatalf("app rows = %d on a partial host, want 0", len(projections[0].AppRows))
	}
	if tally.Preserved != 1 {
		t.Fatalf("tally.Preserved = %d, want 1", tally.Preserved)
	}
}

func TestProjectHostsStaleCommittedCapturePreserves(t *testing.T) {
	apps := []facts.Envelope{
		ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
	}
	observations, _, _, err := ExtractObservations(apps, nil)
	if err != nil {
		t.Fatalf("ExtractObservations: %v", err)
	}
	committedCapture, _ := time.Parse(time.RFC3339, "2026-10-29T22:40:00Z")
	committed := map[string]CommittedHost{
		HostKey(cloud.SSMHostTuple{Partition: "aws", AccountID: "000000000001", Region: "us-east-1", InstanceID: "i-1"}): {
			AppCapture:    committedCapture,
			HasAppCapture: true,
		},
	}
	projections, _ := ProjectHosts(observations, nil, committed, ssmRowsBounds, ssmRowsNow)
	if projections[0].Supersede {
		t.Fatalf("Supersede = true for a capture older than the committed one (delayed duplicate must not retract newer truth)")
	}
}

func TestProjectHostsConflictingPlatformIsAmbiguous(t *testing.T) {
	apps := []facts.Envelope{
		ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
	}
	plats := []facts.Envelope{
		ssmFact("aws_ssm_host_platform", "p1", ssmPlatformPayload("i-1", "ssm_inventory", "2026-10-29T22:31:19Z", "2026-10-29T22:33:02Z"), ssmRowsNow),
		ssmFact("aws_ssm_host_platform", "p2", `{"partition":"aws","account_id":"000000000001","region":"us-east-1","instance_id":"i-1","resource_type":"AWS::EC2::Instance","platform_type":"Linux","platform_name":"OtherLinux","source_type":"ssm_inventory","capture_time":"2026-10-29T22:31:19Z","observation_time":"2026-10-29T22:33:02Z"}`, ssmRowsNow),
	}
	observations, platformObservations, _, err := ExtractObservations(apps, plats)
	if err != nil {
		t.Fatalf("ExtractObservations: %v", err)
	}
	projections, _ := ProjectHosts(observations, platformObservations, nil, ssmRowsBounds, ssmRowsNow)
	got := projections[0]
	if !got.Supersede {
		t.Fatalf("Supersede = false: conflicting platform must not block the coherent application capture")
	}
	if len(got.PlatformRows) != 0 {
		t.Fatalf("platform rows = %d on conflicting platform evidence, want 0 (ambiguous stays unresolved)", len(got.PlatformRows))
	}
	if len(got.EdgeRows) != 0 {
		t.Fatalf("edges = %d without a resolved platform, want 0", len(got.EdgeRows))
	}
	if len(got.AppRows) != 1 {
		t.Fatalf("app rows = %d, want 1", len(got.AppRows))
	}
}

func TestProjectHostsMalformedFactQuarantinedSiblingsProject(t *testing.T) {
	apps := []facts.Envelope{
		ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
		ssmFact("aws_ssm_host_application", "bad", `{"partition":"aws"}`, ssmRowsNow),
	}
	observations, _, quarantined, err := ExtractObservations(apps, nil)
	if err != nil {
		t.Fatalf("ExtractObservations: %v", err)
	}
	if len(quarantined) != 1 || quarantined[0].FactID != "bad" {
		t.Fatalf("quarantined = %+v, want the single malformed fact", quarantined)
	}
	projections, _ := ProjectHosts(observations, nil, nil, ssmRowsBounds, ssmRowsNow)
	if !projections[0].Supersede || len(projections[0].AppRows) != 1 {
		t.Fatalf("valid sibling did not project supersede=%v rows=%d", projections[0].Supersede, len(projections[0].AppRows))
	}
}

func TestProjectHostsUnpairedPlatformWritesNoEdge(t *testing.T) {
	apps := []facts.Envelope{
		ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
	}
	plats := []facts.Envelope{
		ssmFact("aws_ssm_host_platform", "p1", ssmPlatformPayload("i-1", "describe_instance_information", "", "2026-10-29T20:00:00Z"), ssmRowsNow),
	}
	observations, platformObservations, _, err := ExtractObservations(apps, plats)
	if err != nil {
		t.Fatalf("ExtractObservations: %v", err)
	}
	projections, _ := ProjectHosts(observations, platformObservations, nil, ssmRowsBounds, ssmRowsNow)
	got := projections[0]
	if !got.Supersede {
		t.Fatalf("Supersede = false: an out-of-skew platform must not block the coherent application capture")
	}
	if len(got.PlatformRows) != 1 {
		t.Fatalf("platform rows = %d, want 1 (unpaired evidence still projects)", len(got.PlatformRows))
	}
	if len(got.EdgeRows) != 0 {
		t.Fatalf("edges = %d beyond the skew bound, want 0", len(got.EdgeRows))
	}
}

func TestProjectHostsAreIndependentAcrossHosts(t *testing.T) {
	apps := []facts.Envelope{
		ssmFact("aws_ssm_host_application", "a1", ssmAppPayload("i-1", "2026-10-29T22:31:19Z", "alpha"), ssmRowsNow),
		ssmFact("aws_ssm_host_application", "a2", ssmAppPayload("i-2", "2026-10-29T22:35:44Z", "beta"), ssmRowsNow),
		ssmFact("aws_ssm_host_application", "a3", ssmAppPayload("i-2", "2026-10-29T22:31:19Z", "gamma"), ssmRowsNow),
	}
	observations, _, _, err := ExtractObservations(apps, nil)
	if err != nil {
		t.Fatalf("ExtractObservations: %v", err)
	}
	projections, tally := ProjectHosts(observations, nil, nil, ssmRowsBounds, ssmRowsNow)
	if len(projections) != 2 {
		t.Fatalf("projections = %d, want 2 hosts", len(projections))
	}
	if tally.Superseded != 1 || tally.Preserved != 1 {
		t.Fatalf("tally = %+v, want 1 superseded + 1 preserved", tally)
	}
}
