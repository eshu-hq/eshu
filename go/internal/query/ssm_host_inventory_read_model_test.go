// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

var ssmReadNow = time.Date(2026, 10, 29, 23, 0, 0, 0, time.UTC)

var ssmReadBounds = ssmhost.Bounds{
	MaxCaptureAge:             2 * time.Hour,
	MaxCrossTypeSkew:          10 * time.Minute,
	MaxPlatformObservationAge: 24 * time.Hour,
}

func ssmReadAppNode(uid, capture, name string) map[string]any {
	return ssmReadAppNodeForInstance(uid, "i-1", capture, name)
}

func ssmReadAppNodeForInstance(uid, instance, capture, name string) map[string]any {
	return map[string]any{
		"uid": uid, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
		"instance_id": instance, "resource_type": "AWS::EC2::Instance", "source_type": "AWS:Application",
		"schema_version": "1.0.0", "capture_time": capture, "name": name, "version": "1.0",
		"scope_id": "scope-1", "generation_id": "gen-1",
	}
}

func ssmReadPlatformNode(uid, source, observed string) map[string]any {
	return ssmReadPlatformNodeForInstance(uid, "i-1", source, observed)
}

func ssmReadPlatformNodeForInstance(uid, instance, source, observed string) map[string]any {
	return map[string]any{
		"uid": uid, "partition": "aws", "account_id": "000000000001", "region": "us-east-1",
		"instance_id": instance, "resource_type": "AWS::EC2::Instance", "source_type": source,
		"platform_type": "Linux", "platform_name": "ExampleLinux",
		"capture_time": "2026-10-29T22:31:19Z", "observation_time": observed,
		"scope_id": "scope-1", "generation_id": "gen-1",
	}
}

func ssmReadAppFact(factID, capture, name string) ssmAdmittedFact {
	return ssmReadAppFactForInstance(factID, "i-1", capture, name)
}

func ssmReadAppFactForInstance(factID, instance, capture, name string) ssmAdmittedFact {
	return ssmAdmittedFact{
		FactID: factID, FactKind: "aws_ssm_host_application", ScopeID: "scope-1", GenerationID: "gen-2",
		Payload: map[string]any{
			"partition": "aws", "account_id": "000000000001", "region": "us-east-1", "instance_id": instance,
			"resource_type": "AWS::EC2::Instance", "source_type": "AWS:Application", "schema_version": "1.0.0",
			"capture_time": capture, "name": name, "version": "1.0",
		},
	}
}

func ssmReadPlatformFact(factID, source, capture, observed string) ssmAdmittedFact {
	payload := map[string]any{
		"partition": "aws", "account_id": "000000000001", "region": "us-east-1", "instance_id": "i-1",
		"resource_type": "AWS::EC2::Instance", "source_type": source,
		"platform_type": "Linux", "platform_name": "ExampleLinux", "observation_time": observed,
	}
	if capture != "" {
		payload["capture_time"] = capture
	}
	return ssmAdmittedFact{FactID: factID, FactKind: "aws_ssm_host_platform", ScopeID: "scope-1", GenerationID: "gen-2", Payload: payload}
}

// stubSSMGraph scripts the committed-read legs separately, dispatching on
// the RETURN alias each shipped query uses: tuple seeds (h_partition), the
// paired app-node rows (AS app), and the platform-node rows (plat_only). A
// leg with no scripted rows returns none, so a test that forgets a leg fails
// instead of passing on a wrong row shape.
type stubSSMGraph struct {
	seedAppRows  []map[string]any
	seedPlatRows []map[string]any
	appRows      []map[string]any
	platRows     []map[string]any
	err          error
}

func (s *stubSSMGraph) Run(_ context.Context, query string, _ map[string]any) ([]map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	switch {
	case strings.Contains(query, "plat_only"):
		return s.platRows, nil
	case strings.Contains(query, "AS app"):
		return s.appRows, nil
	case strings.Contains(query, "h_partition"):
		if strings.Contains(query, ":SSMHostPlatform") {
			return s.seedPlatRows, nil
		}
		return s.seedAppRows, nil
	default:
		return nil, nil
	}
}

func (s *stubSSMGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

type stubSSMFacts struct {
	facts []ssmAdmittedFact
	err   error
}

func (s *stubSSMFacts) SSMHostActiveFacts(context.Context, ssmHostInventoryFilter) ([]ssmAdmittedFact, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.facts, nil
}

// ssmSeedTuples reduces node maps to the bare host tuples the shipped seed
// queries return (h_partition and siblings, never full nodes).
func ssmSeedTuples(nodes []map[string]any) []map[string]any {
	var seeds []map[string]any
	seen := map[string]bool{}
	for _, node := range nodes {
		key := ssmAdmittedHostKey(node)
		if seen[key] {
			continue
		}
		seen[key] = true
		seeds = append(seeds, map[string]any{
			"h_partition":   stringFromMap(node, "partition"),
			"h_account_id":  stringFromMap(node, "account_id"),
			"h_region":      stringFromMap(node, "region"),
			"h_instance_id": stringFromMap(node, "instance_id"),
		})
	}
	return seeds
}

// ssmCommittedGraph scripts all four committed-read legs in the exact row
// shapes the shipped Cypher returns: bare-tuple seeds per label, one
// {app, plat} row per application node with the same-host platform paired on
// the OPTIONAL MATCH leg (nil when the host has no platform node), and one
// {plat_only} row per platform node on the platform leg (the read merges
// same-host platforms by uid and serves host-less platforms as
// platform-only committed hosts).
func ssmCommittedGraph(apps, plats []map[string]any) *stubSSMGraph {
	platsByHost := map[string][]map[string]any{}
	for _, plat := range plats {
		key := ssmAdmittedHostKey(plat)
		platsByHost[key] = append(platsByHost[key], plat)
	}
	var appRows []map[string]any
	for _, app := range apps {
		// An unpaired host carries an explicit null plat, exactly like an
		// OPTIONAL MATCH miss (a typed nil map would read non-nil).
		row := map[string]any{"app": app, "plat": nil}
		if candidates := platsByHost[ssmAdmittedHostKey(app)]; len(candidates) > 0 {
			row["plat"] = candidates[0]
		}
		appRows = append(appRows, row)
	}
	var platRows []map[string]any
	for _, plat := range plats {
		platRows = append(platRows, map[string]any{"plat_only": plat})
	}
	return &stubSSMGraph{
		seedAppRows:  ssmSeedTuples(apps),
		seedPlatRows: ssmSeedTuples(plats),
		appRows:      appRows,
		platRows:     platRows,
	}
}

func TestReadSSMHostInventoryPositiveAgrees(t *testing.T) {
	graph := ssmCommittedGraph(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		[]map[string]any{ssmReadPlatformNode("uid-plat-1", "ssm_inventory", "2026-10-29T22:33:02Z")},
	)
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T22:31:19Z", "alpha"),
		ssmReadPlatformFact("p1", "ssm_inventory", "2026-10-29T22:31:19Z", "2026-10-29T22:33:02Z"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(model.Hosts))
	}
	host := model.Hosts[0]
	if host.Status != "successful" {
		t.Fatalf("status = %q, want successful", host.Status)
	}
	if len(host.Applications) != 1 || host.Applications[0]["name"] != "alpha" {
		t.Fatalf("applications = %v, want the committed alpha row", host.Applications)
	}
	if len(host.Platforms) != 1 {
		t.Fatalf("platforms = %d, want 1", len(host.Platforms))
	}
	if host.CaptureTime != "2026-10-29T22:31:19Z" {
		t.Fatalf("capture_time = %q, want the committed capture", host.CaptureTime)
	}
	// Observations must never synthesize findings: the whole host view
	// marshals without any vulnerability, package-correlation, or
	// supply-chain key (#9209 owns correlation, never this surface).
	encoded, err := json.Marshal(host)
	if err != nil {
		t.Fatalf("marshal host view: %v", err)
	}
	for _, key := range []string{"vulnerability", "os_package", "supply_chain_impact", "purl", "image_digest"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Fatalf("host view carries %q: %s", key, encoded)
		}
	}
}

func TestReadSSMHostInventoryPartialServesCommitted(t *testing.T) {
	graph := ssmCommittedGraph(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		nil,
	)
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T22:31:19Z", "alpha"),
		ssmReadAppFact("a2", "2026-10-29T22:35:44Z", "beta"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1 (prior truth preserved and served)", len(model.Hosts))
	}
	if model.Hosts[0].Status != "partial" {
		t.Fatalf("status = %q, want partial", model.Hosts[0].Status)
	}
	if len(model.Hosts[0].Applications) != 1 {
		t.Fatalf("applications = %d, want the 1 committed row (never the incoherent mix)", len(model.Hosts[0].Applications))
	}
}

func TestReadSSMHostInventorySilentIsUnknown(t *testing.T) {
	graph := ssmCommittedGraph(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		nil,
	)
	store := &stubSSMFacts{}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 || model.Hosts[0].Status != "unknown" {
		t.Fatalf("silent generation must preserve prior truth as unknown, got %+v", model.Hosts)
	}
}

func TestReadSSMHostInventoryStaleCommitted(t *testing.T) {
	graph := ssmCommittedGraph(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T20:00:00Z", "alpha")},
		nil,
	)
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T20:00:00Z", "alpha"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 || model.Hosts[0].Status != "stale" {
		t.Fatalf("aged committed capture must read stale, got %+v", model.Hosts)
	}
}

func TestReadSSMHostInventoryPendingProjection(t *testing.T) {
	graph := ssmCommittedGraph(
		[]map[string]any{ssmReadAppNode("uid-app-old", "2026-10-29T22:31:19Z", "alpha")},
		nil,
	)
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T22:45:00Z", "alpha"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 || model.Hosts[0].Status != "unknown" {
		t.Fatalf("newer admitted capture than committed must read unknown (projection pending), got %+v", model.Hosts)
	}
	if len(model.Hosts[0].Applications) != 1 {
		t.Fatalf("pending projection must still serve committed rows, got %d", len(model.Hosts[0].Applications))
	}
}

func TestReadSSMHostInventoryAbsentHostIsAbsent(t *testing.T) {
	model, err := ReadSSMHostInventory(context.Background(), &stubSSMGraph{}, &stubSSMFacts{}, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 0 {
		t.Fatalf("hosts = %d with no committed nodes and no facts, want 0 (never an empty host)", len(model.Hosts))
	}
}

func TestReadSSMHostInventoryBoundsApplications(t *testing.T) {
	// Distinct uids: one committed capture holds one row per application,
	// so the served-list cap is exercised by row count, never by duplicates
	// (the read deduplicates the OPTIONAL MATCH fan-out by uid).
	var apps []map[string]any
	for i := 0; i < 250; i++ {
		apps = append(apps, ssmReadAppNode(fmt.Sprintf("uid-app-%d", i), "2026-10-29T22:31:19Z", "alpha"))
	}
	graph := ssmCommittedGraph(apps, nil)
	store := &stubSSMFacts{facts: []ssmAdmittedFact{
		ssmReadAppFact("a1", "2026-10-29T22:31:19Z", "alpha"),
	}}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts[0].Applications) != ssmHostInventoryMaxApplications {
		t.Fatalf("applications = %d, want the %d cap", len(model.Hosts[0].Applications), ssmHostInventoryMaxApplications)
	}
	if !model.Hosts[0].ApplicationsTruncated {
		t.Fatalf("ApplicationsTruncated = false on a capped list")
	}
}

func TestReadSSMHostInventoryOverWindowHostReadsUnknownTruncated(t *testing.T) {
	graph := ssmCommittedGraph(
		[]map[string]any{ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")},
		nil,
	)
	var facts []ssmAdmittedFact
	// One malformed sibling for the same host, inside the window: a
	// wrong-typed required field quarantines per fact and counts on the host
	// view, never fails the read. (Rows past the window are unobserved:
	// neither assessed nor counted; the truncation flag covers them.)
	malformed := ssmReadAppFact("bad", "2026-10-29T22:31:19Z", "alpha")
	malformed.Payload["capture_time"] = 123
	facts = append(facts, malformed)
	for i := 0; i < ssmHostInventoryMaxFactRowsPerHost+1; i++ {
		facts = append(facts, ssmReadAppFact(fmt.Sprintf("a%d", i), "2026-10-29T22:31:19Z", "alpha"))
	}
	store := &stubSSMFacts{facts: facts}
	model, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(model.Hosts))
	}
	host := model.Hosts[0]
	if !host.FactsTruncated {
		t.Fatalf("FactsTruncated = false on a host past the %d-row admitted-fact window", ssmHostInventoryMaxFactRowsPerHost)
	}
	if host.Status != "unknown" || host.StatusReason != "admitted-fact window exhausted before coherence" {
		t.Fatalf("status = %q reason = %q, want unknown with the truncated-facts reason (never a coherence verdict on a truncated stream)", host.Status, host.StatusReason)
	}
	if host.MalformedFacts != 1 {
		t.Fatalf("host malformed_facts = %d, want 1", host.MalformedFacts)
	}
	if model.MalformedFacts != 1 {
		t.Fatalf("model malformed_facts = %d, want 1", model.MalformedFacts)
	}
}

func TestReadSSMHostInventoryActiveOnlyHostsArePaged(t *testing.T) {
	// Fifteen active-only hosts (no committed nodes) at Limit 10: hosts are
	// the page unit, so the first page serves exactly 10 with a cursor and
	// the second serves the remaining 5 with no cursor.
	graph := &stubSSMGraph{}
	var facts []ssmAdmittedFact
	for i := 0; i < 15; i++ {
		instance := fmt.Sprintf("i-%d", i)
		facts = append(facts, ssmReadAppFactForInstance(fmt.Sprintf("f%d", i), instance, "2026-10-29T22:31:19Z", "alpha"))
	}
	store := &stubSSMFacts{facts: facts}
	first, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(first.Hosts) != 10 {
		t.Fatalf("first page hosts = %d, want 10 (limit bounds active-only hosts too)", len(first.Hosts))
	}
	if first.NextCursor != "10" {
		t.Fatalf("first page next_cursor = %q, want %q", first.NextCursor, "10")
	}
	second, err := ReadSSMHostInventory(context.Background(), graph, store, ssmHostInventoryFilter{Limit: 10, Offset: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(second.Hosts) != 5 {
		t.Fatalf("second page hosts = %d, want 5", len(second.Hosts))
	}
	if second.NextCursor != "" {
		t.Fatalf("second page next_cursor = %q, want empty (no more hosts)", second.NextCursor)
	}
	seen := map[string]bool{}
	for _, host := range append(first.Hosts, second.Hosts...) {
		if seen[host.InstanceID] {
			t.Fatalf("host %q served on both pages", host.InstanceID)
		}
		seen[host.InstanceID] = true
	}
	if len(seen) != 15 {
		t.Fatalf("distinct hosts across pages = %d, want 15", len(seen))
	}
}

func TestReadSSMHostInventoryServesPlatformOnlyCommittedHost(t *testing.T) {
	// A DescribeInstanceInformation-only committed host (platform node, no
	// application node) must appear with its platform row: the committed
	// read serves platform-only hosts on their own leg, never drops them
	// after they consume a seed page slot.
	plat := ssmReadPlatformNodeForInstance("uid-plat-9", "i-9", "describe_instance_information", "2026-10-29T22:50:00Z")
	graph := ssmCommittedGraph(nil, []map[string]any{plat})
	model, err := ReadSSMHostInventory(context.Background(), graph, &stubSSMFacts{}, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1 (the platform-only committed host)", len(model.Hosts))
	}
	host := model.Hosts[0]
	if host.InstanceID != "i-9" {
		t.Fatalf("instance_id = %q, want i-9", host.InstanceID)
	}
	if len(host.Applications) != 0 {
		t.Fatalf("applications = %d, want 0 (no application node committed)", len(host.Applications))
	}
	if len(host.Platforms) != 1 {
		t.Fatalf("platforms = %d, want the 1 committed platform row", len(host.Platforms))
	}
}

func TestSSMHostPageBoundsClampsAndDetectsOverflow(t *testing.T) {
	offset, limit, prefix, ok := ssmHostPageBounds(ssmHostInventoryFilter{Limit: 10, Offset: 5})
	if !ok || offset != 5 || limit != 10 || prefix != 16 {
		t.Fatalf("bounds = (%d, %d, %d, %v), want (5, 10, 16, true)", offset, limit, prefix, ok)
	}
	if offset, limit, prefix, ok := ssmHostPageBounds(ssmHostInventoryFilter{Offset: -3}); !ok || offset != 0 || limit != ssmHostInventoryDefaultLimit || prefix != ssmHostInventoryDefaultLimit+1 {
		t.Fatalf("negative offset bounds = (%d, %d, %d, %v), want clamp to zero with default limit", offset, limit, prefix, ok)
	}
	if _, _, _, ok := ssmHostPageBounds(ssmHostInventoryFilter{Limit: 10, Offset: int(^uint(0) >> 1)}); ok {
		t.Fatalf("overflowed cursor bounds ok = true, want false (beyond-the-end empty page)")
	}
}

func TestReadSSMHostInventoryDeduplicatesPairedFanOut(t *testing.T) {
	// One application node paired with two platforms fans out to two
	// {app, plat} rows on the OPTIONAL MATCH leg; the read serves the app
	// once with both platforms.
	app := ssmReadAppNode("uid-app-1", "2026-10-29T22:31:19Z", "alpha")
	platInventory := ssmReadPlatformNode("uid-plat-1", "ssm_inventory", "2026-10-29T22:33:02Z")
	platDII := ssmReadPlatformNodeForInstance("uid-plat-2", "i-1", "describe_instance_information", "2026-10-29T22:50:00Z")
	graph := &stubSSMGraph{
		seedAppRows:  ssmSeedTuples([]map[string]any{app}),
		seedPlatRows: ssmSeedTuples([]map[string]any{platInventory, platDII}),
		appRows: []map[string]any{
			{"app": app, "plat": platInventory},
			{"app": app, "plat": platDII},
		},
		platRows: []map[string]any{
			{"plat_only": platInventory},
			{"plat_only": platDII},
		},
	}
	model, err := ReadSSMHostInventory(context.Background(), graph, &stubSSMFacts{}, ssmHostInventoryFilter{Limit: 10}, ssmReadBounds, ssmReadNow)
	if err != nil {
		t.Fatalf("ReadSSMHostInventory: %v", err)
	}
	if len(model.Hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(model.Hosts))
	}
	host := model.Hosts[0]
	if len(host.Applications) != 1 {
		t.Fatalf("applications = %d, want 1 (fan-out deduplicated by uid)", len(host.Applications))
	}
	if len(host.Platforms) != 2 {
		t.Fatalf("platforms = %d, want 2 (both paired provenances)", len(host.Platforms))
	}
}

func TestBuildSSMHostActiveFactsSQL(t *testing.T) {
	filter := ssmHostInventoryFilter{InstanceID: "i-1", Limit: 10}
	sqlQuery, args := buildSSMHostActiveFactsSQL(filter)
	for _, want := range []string{
		"fact_records.fact_kind IN",
		"aws_ssm_host_application",
		"aws_ssm_host_platform",
		"active",
		"payload->>'instance_id'",
	} {
		if !strings.Contains(sqlQuery, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sqlQuery)
		}
	}
	if len(args) == 0 {
		t.Fatalf("SQL has no args")
	}
	// The trailing bounds keep one row past the per-host cap (so the
	// assembler can tell an exhausted window from a complete one) and cap
	// the host count to the page prefix (so active-only hosts stay bounded).
	if len(args) < 2 {
		t.Fatalf("SQL args = %d, want at least the window and host-rank bounds", len(args))
	}
	if window, ok := args[len(args)-2].(int); !ok || window != ssmHostInventoryMaxFactRowsPerHost+1 {
		t.Fatalf("window bound = %v, want %d (cap plus the exhaustion signal row)", args[len(args)-2], ssmHostInventoryMaxFactRowsPerHost+1)
	}
	// Filter carries Limit 10 and no offset: the host-rank budget is the
	// page prefix 0+10+1.
	if budget, ok := args[len(args)-1].(int); !ok || budget != 11 {
		t.Fatalf("host-rank budget = %v, want 11 (offset+limit+1)", args[len(args)-1])
	}
	for _, want := range []string{"host_row_number", "host_rank", "DENSE_RANK"} {
		if !strings.Contains(sqlQuery, want) {
			t.Fatalf("SQL missing %q:\n%s", want, sqlQuery)
		}
	}
}
