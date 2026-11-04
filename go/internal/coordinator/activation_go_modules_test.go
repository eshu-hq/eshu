// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	testGoCommit       = "0123456789abcdef0123456789abcdef01234567"
	testGoHostScope    = "go-reachability://fleet"
	testGoInstanceID   = "scanner-go-reachability"
	testGoModulesBlock = `{"derive_from_go_modules":{"enabled":true}}`
)

// fakeGoModuleRepositoryReader pages a fixture fleet the way the SQL read does:
// one page per rotation bucket over every candidate, keeping only Go repositories.
type fakeGoModuleRepositoryReader struct {
	fleet   []goFleetRepository
	planned map[string]bool
	err     error
	filters []scannercomponent.GoModuleRepositoryFilter
}

type goFleetRepository struct {
	scannercomponent.GoModuleRepository
	hasGoModule bool
}

func (f *fakeGoModuleRepositoryReader) ListGoModuleRepositories(
	_ context.Context,
	filter scannercomponent.GoModuleRepositoryFilter,
) (scannercomponent.GoModuleRepositoryPage, error) {
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return scannercomponent.GoModuleRepositoryPage{}, f.err
	}
	if f.planned[filter.RunID] {
		return scannercomponent.GoModuleRepositoryPage{AlreadyPlanned: true}, nil
	}
	slot, slots := scannercomponent.GoModulePageWindow(filter.RotationBucket, len(f.fleet), filter.PageSize)
	page := scannercomponent.GoModuleRepositoryPage{TotalCandidates: len(f.fleet), Slot: slot, SlotCount: slots}
	for i, repository := range f.fleet {
		if i/filter.PageSize != slot {
			continue
		}
		page.Candidates++
		if repository.hasGoModule {
			page.Repositories = append(page.Repositories, repository.GoModuleRepository)
		}
	}
	return page, nil
}

func goFleet(size int) []goFleetRepository {
	fleet := make([]goFleetRepository, 0, size)
	for i := range size {
		fleet = append(fleet, goFleetRepository{
			GoModuleRepository: scannercomponent.GoModuleRepository{
				RepositoryID:    fmt.Sprintf("repository:r_%08x", i),
				RemoteURL:       fmt.Sprintf("https://github.com/eshu-hq/repo-%d", i),
				SourceCommitSHA: testGoCommit,
			},
			hasGoModule: true,
		})
	}
	return fleet
}

func goModulesActivationConfiguration(block, source, kind string) string {
	raw := `{
		"schema_version":"eshu.component.instance.v1",
		"component_id":"` + scannercomponent.IDGoReachability + `",
		"component_version":"0.1.0",
		"publisher":"eshu-hq",
		"manifest_digest":"sha256:1234",
		"config_handle":"component-config:abcd",
		"host":{"source_system":"` + source + `","scope":{"id":"` + testGoHostScope + `","kind":"` + kind + `"}},
		"runtime":{"sdk_protocol":"collector-sdk/v1alpha3","adapter":"process"}`
	if block != "" {
		raw += `,"component_config":` + block
	}
	return raw + `}`
}

func goModulesInstance(now time.Time, configuration string) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID: testGoInstanceID, CollectorKind: scope.CollectorScannerWorker,
		Mode: workflow.CollectorModeScheduled, Enabled: true, ClaimsEnabled: true,
		Configuration: configuration, CreatedAt: now, UpdatedAt: now, LastObservedAt: now,
	}
}

func goModulesService(t *testing.T, now *time.Time, store *fakeStore, reader GoModuleRepositoryReader, egressPolicy string) Service {
	t.Helper()
	service := Service{
		Config: Config{
			DeploymentMode: deploymentModeActive, ClaimsEnabled: true, ReconcileInterval: time.Hour,
		},
		Store:                     store,
		ComponentExtensionPlanner: extension.WorkPlanner{},
		GoModuleRepositoryReader:  reader,
		GovernanceAudit:           &fakeGovernanceAuditAppender{},
		Clock:                     func() time.Time { return *now },
	}
	if egressPolicy != "" {
		service.Config.ExtensionEgressPolicy = mustParseExtensionEgressPolicy(t, egressPolicy)
	}
	return service
}

func scheduleGoModules(t *testing.T, service Service, now time.Time, instance workflow.CollectorInstance) error {
	t.Helper()
	return service.scheduleComponentExtensionWork(context.Background(), now, []workflow.CollectorInstance{instance})
}

func enqueuedScopes(store *fakeStore) map[string]workflow.WorkItem {
	scopes := make(map[string]workflow.WorkItem, len(store.enqueuedItems))
	for _, item := range store.enqueuedItems {
		scopes[item.ScopeID] = item
	}
	return scopes
}

// TestGoModulesDerivationPlansOneScopePerGoRepository is the #10027 contract: a
// Go repository gets a scope carrying its commit and clone URL in the run, and a
// repository without a go.mod never gets one. The host scope names no repository,
// so no work item is planned for it.
func TestGoModulesDerivationPlansOneScopePerGoRepository(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	fleet := goFleet(3)
	fleet[1].hasGoModule = false
	reader := &fakeGoModuleRepositoryReader{fleet: fleet}
	store := &fakeStore{}
	service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	scopes := enqueuedScopes(store)
	if len(scopes) != 2 {
		t.Fatalf("enqueued scopes = %v, want the two Go repositories only", keysOf(scopes))
	}
	if _, ok := scopes[testGoHostScope]; ok {
		t.Fatal("the host scope was planned, want derived repository scopes only")
	}
	if _, ok := scopes["go-reachability://repository:r_00000001"]; ok {
		t.Fatal("a repository without a go.mod was planned")
	}
	item, ok := scopes["go-reachability://repository:r_00000000"]
	if !ok {
		t.Fatalf("enqueued scopes = %v, want repository r_00000000", keysOf(scopes))
	}
	if len(store.createdRuns) != 1 {
		t.Fatalf("runs = %d, want 1", len(store.createdRuns))
	}
	target, err := scannercomponent.ResolveGoReachabilityTarget(store.createdRuns[0].RequestedScopeSet, item.ScopeID)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if target.SourceCommitSHA != testGoCommit || target.CloneURL != "https://github.com/eshu-hq/repo-0" {
		t.Fatalf("target = %+v", target)
	}
}

// TestGoModulesDerivationPagesAndRotatesTheWholeFleet proves one pass plans at
// most 256 repositories, and consecutive buckets visit every repository once.
func TestGoModulesDerivationPagesAndRotatesTheWholeFleet(t *testing.T) {
	t.Parallel()

	const fleetSize = 600
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(fleetSize)}
	seen := map[string]int{}
	for bucket := range 3 {
		store := &fakeStore{}
		at := now.Add(time.Duration(bucket) * time.Hour)
		service := goModulesService(t, &at, store, reader, `{"mode":"broad"}`)
		instance := goModulesInstance(at, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))
		if err := scheduleGoModules(t, service, at, instance); err != nil {
			t.Fatalf("bucket %d: %v", bucket, err)
		}
		if got := len(store.enqueuedItems); got > scannercomponent.GoModulePageSize {
			t.Fatalf("bucket %d planned %d items, over the page size", bucket, got)
		}
		for _, item := range store.enqueuedItems {
			seen[item.ScopeID]++
		}
	}
	if len(seen) != fleetSize {
		t.Fatalf("visited %d repositories in one rotation, want %d", len(seen), fleetSize)
	}
	for scopeID, count := range seen {
		if count != 1 {
			t.Fatalf("scope %q planned %d times in one rotation", scopeID, count)
		}
	}
	for _, filter := range reader.filters {
		if filter.PageSize != scannercomponent.GoModulePageSize {
			t.Fatalf("page size = %d, want %d", filter.PageSize, scannercomponent.GoModulePageSize)
		}
	}
	if reader.filters[1].RotationBucket != reader.filters[0].RotationBucket+1 {
		t.Fatalf("buckets = %d, %d, want consecutive", reader.filters[0].RotationBucket, reader.filters[1].RotationBucket)
	}
	if got, want := reader.filters[0].RotationBucket, schedule.DerivedTargetRotationBucket(now, time.Hour); got != want {
		t.Fatalf("bucket = %d, want %d (the plan key basis)", got, want)
	}
}

// TestGoModulesDerivationRepeatPassPlansNothing proves a page stays fixed for
// its bucket: once its run exists, a repeat pass reads no candidates and admits
// nothing, even when a repository's commit moved.
func TestGoModulesDerivationRepeatPassPlansNothing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(2), planned: map[string]bool{}}
	store := &fakeStore{}
	service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))
	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatal(err)
	}
	reader.planned[reader.filters[0].RunID] = true
	reader.fleet[0].SourceCommitSHA = "fedcba9876543210fedcba9876543210fedcba98"
	later := now.Add(10 * time.Minute)
	if err := scheduleGoModules(t, service, later, instance); err != nil {
		t.Fatal(err)
	}
	if len(store.enqueuedItems) != 2 || len(store.createdRuns) != 1 {
		t.Fatalf("items = %d runs = %d after a repeat pass, want 2 and 1", len(store.enqueuedItems), len(store.createdRuns))
	}
	if reader.filters[0].RunID != reader.filters[1].RunID {
		t.Fatalf("run ids differ inside one bucket: %q vs %q", reader.filters[0].RunID, reader.filters[1].RunID)
	}
}

// TestGoModulesDerivationHonorsEgressDeny proves a denied activation reads
// nothing and plans nothing: the derivation sits behind the egress decision.
func TestGoModulesDerivationHonorsEgressDeny(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(2)}
	store := &fakeStore{}
	service := goModulesService(t, &now, store, reader, "")
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(store.enqueuedItems) != 0 || len(reader.filters) != 0 {
		t.Fatalf("items = %d reads = %d under egress deny, want 0 and 0", len(store.enqueuedItems), len(reader.filters))
	}
}

// TestGoModulesDerivationRefusesAMislabeledHost proves a derivation under the
// wrong host fails with a bounded log line and reads nothing.
func TestGoModulesDerivationRefusesAMislabeledHost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, source, kind string }{
		{"scope kind", "scanner_worker", "repository"},
		{"source system", "git", "scanner_worker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
			reader := &fakeGoModuleRepositoryReader{fleet: goFleet(1)}
			store := &fakeStore{}
			var logs bytes.Buffer
			service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
			service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, tc.source, tc.kind))

			err := scheduleGoModules(t, service, now, instance)
			if err == nil || !strings.Contains(err.Error(), `must both be "scanner_worker"`) {
				t.Fatalf("schedule error = %v, want the host refusal", err)
			}
			if len(reader.filters) != 0 {
				t.Fatalf("reads = %d after refusal, want 0", len(reader.filters))
			}
			for _, want := range []string{"refused derived scopes", `"reason":"host_scope_kind_or_source_system_mismatch"`, `"level":"ERROR"`} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("logs = %s, want %s", logs.String(), want)
				}
			}
		})
	}
}

// TestGoModulesDerivationLeavesOtherScannerActivationsAlone proves the image
// scanner keeps its host scope and that no other activation reads the fleet,
// even if its config carries a derive-shaped block.
func TestGoModulesDerivationLeavesOtherScannerActivationsAlone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(1)}
	store := &fakeStore{}
	service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
	configuration := strings.ReplaceAll(
		goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"),
		scannercomponent.IDGoReachability, scannercomponent.IDImage,
	)
	if err := scheduleGoModules(t, service, now, goModulesInstance(now, configuration)); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(reader.filters) != 0 {
		t.Fatalf("reads = %d, want 0 for another scanner", len(reader.filters))
	}
	if _, ok := enqueuedScopes(store)[testGoHostScope]; !ok || len(store.enqueuedItems) != 1 {
		t.Fatalf("enqueued = %v, want the host scope only", keysOf(enqueuedScopes(store)))
	}
}

// TestGoModulesDerivationDisabledKeepsTheHostScope proves the host scope is
// suppressed only while the derivation is on.
func TestGoModulesDerivationDisabledKeepsTheHostScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(1)}
	store := &fakeStore{}
	service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
	instance := goModulesInstance(now, goModulesActivationConfiguration(
		`{"derive_from_go_modules":{"enabled":false}}`, "scanner_worker", "scanner_worker"))
	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(reader.filters) != 0 {
		t.Fatalf("reads = %d, want 0 when disabled", len(reader.filters))
	}
	if _, ok := enqueuedScopes(store)[testGoHostScope]; !ok {
		t.Fatalf("enqueued = %v, want the host scope", keysOf(enqueuedScopes(store)))
	}
}

// TestGoModulesDerivationBootstrapInstanceStillRotates proves the fixed bootstrap
// plan key does not pin an instance to the first page of the fleet.
func TestGoModulesDerivationBootstrapInstanceStillRotates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	reader := &fakeGoModuleRepositoryReader{fleet: goFleet(1)}
	store := &fakeStore{}
	service := goModulesService(t, &now, store, reader, `{"mode":"broad"}`)
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))
	instance.Bootstrap = true
	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	if err := scheduleGoModules(t, service, later, instance); err != nil {
		t.Fatal(err)
	}
	if reader.filters[0].RunID == reader.filters[1].RunID {
		t.Fatalf("bootstrap run id %q did not change across buckets", reader.filters[0].RunID)
	}
}

func TestGoModulesDerivationRequiresAReaderAndSurfacesReadErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	store := &fakeStore{}
	noReader := goModulesService(t, &now, store, nil, `{"mode":"broad"}`)
	if err := scheduleGoModules(t, noReader, now, instance); err == nil || !strings.Contains(err.Error(), "reader is required") {
		t.Fatalf("schedule error = %v, want a missing-reader error", err)
	}

	sentinel := errors.New("statement timeout")
	failing := goModulesService(t, &now, store, &fakeGoModuleRepositoryReader{err: sentinel}, `{"mode":"broad"}`)
	if err := scheduleGoModules(t, failing, now, instance); !errors.Is(err, sentinel) {
		t.Fatalf("schedule error = %v, want the read error", err)
	}
	if len(store.enqueuedItems) != 0 {
		t.Fatalf("items = %d after failures, want 0", len(store.enqueuedItems))
	}
}

// TestGoModulesDerivationTelemetryHoldsNoRepositoryValue proves the counter uses
// bounded labels and that neither it nor the log line holds a repository id or URL.
func TestGoModulesDerivationTelemetryHoldsNoRepositoryValue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	fleet := goFleet(2)
	fleet = append(fleet, goFleetRepository{
		GoModuleRepository: scannercomponent.GoModuleRepository{
			RepositoryID: "repository:r_000000ff", RemoteURL: "/srv/local", SourceCommitSHA: testGoCommit,
		},
		hasGoModule: true,
	})
	store := &fakeStore{}
	counter := &fakeActivationDerivedScopeCounter{}
	var logs bytes.Buffer
	service := goModulesService(t, &now, store, &fakeGoModuleRepositoryReader{fleet: fleet}, `{"mode":"broad"}`)
	service.ActivationDerivedScopes = counter
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	instance := goModulesInstance(now, goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker"))

	if err := scheduleGoModules(t, service, now, instance); err != nil {
		t.Fatalf("schedule error = %v", err)
	}
	if len(counter.adds) != 1 {
		t.Fatalf("counter adds = %d, want 1", len(counter.adds))
	}
	add := counter.adds[0]
	if add.value != 2 || add.attributes["ecosystem"] != "go" || add.attributes["collector_kind"] != "scanner_worker" {
		t.Fatalf("counter add = %+v", add)
	}
	encoded, err := json.Marshal(add.attributes)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"repository:r_", "github.com", "/srv/local", "go-reachability://"} {
		if strings.Contains(string(encoded), leak) {
			t.Fatalf("counter labels %s carry %q", encoded, leak)
		}
		if strings.Contains(logs.String(), leak) {
			t.Fatalf("logs %s carry %q", logs.String(), leak)
		}
	}
	for _, want := range []string{
		"planned derived scopes", `"planned_work_items":2`, `"candidates":3`, `"total_candidates":3`,
		`"slot":0`, `"slot_count":1`, `"skipped_unsupported_remote":1`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs = %s, want %s", logs.String(), want)
		}
	}
}

// TestDesiredInstancesCarryOnlyTheGoReachabilityConfigBlock proves the readback
// keeps the block for this component alone: the three published scanners' stored
// configuration is unchanged.
func TestDesiredInstancesCarryOnlyTheGoReachabilityConfigBlock(t *testing.T) {
	t.Parallel()

	path := writeActivationFile(t, `host:
  sourceSystem: scanner_worker
  scope:
    id: go-reachability://fleet
    kind: scanner_worker
config:
  derive_from_go_modules:
    enabled: true
`)
	for _, tc := range []struct {
		id    string
		carry bool
	}{
		{scannercomponent.IDGoReachability, true},
		{scannercomponent.IDImage, false},
	} {
		manifest := testActivationManifest(string(scope.CollectorScannerWorker))
		manifest.Metadata.ID = tc.id
		instances, _, err := desiredInstancesForComponent(testActivationReadbackEntry(path), manifest)
		if err != nil {
			t.Fatalf("%s: error = %v", tc.id, err)
		}
		if got := strings.Contains(instances[0].Configuration, `"derive_from_go_modules"`); got != tc.carry {
			t.Fatalf("%s: configuration = %s, want carried %v", tc.id, instances[0].Configuration, tc.carry)
		}
	}
}

// TestGoModulesStartupRefusesABadBlock proves a static or registry activation
// fails at load, before any durable write, as the vulnerability block does.
func TestGoModulesStartupRefusesABadBlock(t *testing.T) {
	t.Parallel()

	for name, configuration := range map[string]string{
		"host":  goModulesActivationConfiguration(testGoModulesBlock, "git", "scanner_worker"),
		"shape": goModulesActivationConfiguration(`{"derive_from_go_modules":true}`, "scanner_worker", "scanner_worker"),
	} {
		if err := checkActivationDocumentDerivation(scope.CollectorScannerWorker, configuration); err == nil {
			t.Errorf("%s: check = nil, want refusal", name)
		}
	}
	good := goModulesActivationConfiguration(testGoModulesBlock, "scanner_worker", "scanner_worker")
	if err := checkActivationDocumentDerivation(scope.CollectorScannerWorker, good); err != nil {
		t.Fatalf("good block: %v", err)
	}
}
