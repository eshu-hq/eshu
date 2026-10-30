// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

// Closed SSM host inventory statuses. Status is always derived at read time
// from committed graph nodes overlaid with the active generation's admitted
// facts, never stored, so the graph and this surface agree by construction.
const (
	ssmHostStatusSuccessful = "successful"
	ssmHostStatusPartial    = "partial"
	ssmHostStatusStale      = "stale"
	ssmHostStatusUnknown    = "unknown"
)

// Closed status reasons. Reasons are static strings: capture times and host
// identities travel as data fields, never inside reason text.
const (
	ssmHostReasonProjected      = "complete capture projected"
	ssmHostReasonPartial        = "latest capture incomplete or incoherent"
	ssmHostReasonStale          = "committed capture past the stale-age bound"
	ssmHostReasonPending        = "newer capture admitted, projection pending"
	ssmHostReasonSilent         = "no capture in the active generation"
	ssmHostReasonTruncatedFacts = "admitted-fact window exhausted before coherence"
)

// ssmCommittedHostsCypher reads one host page's committed observation nodes
// and their pairing edges in a single round trip. Hosts page first (DISTINCT
// tuples, deterministic order, SKIP/LIMIT), then nodes and edges follow their
// hosts, so a host never splits across a page. Status filters apply after
// paging in Go (status needs the admitted-fact overlay), so a status-filtered
// page may be short.
const ssmCommittedHostsCypher = `MATCH (seed)
WHERE (seed:SSMHostApplication OR seed:SSMHostPlatform)
  AND ($all_scopes OR seed.scope_id IN $scope_ids)
  AND ($instance_id = '' OR seed.instance_id = $instance_id)
  AND ($account_id = '' OR seed.account_id = $account_id)
  AND ($region = '' OR seed.region = $region)
WITH DISTINCT seed.partition AS h_partition, seed.account_id AS h_account_id, seed.region AS h_region, seed.instance_id AS h_instance_id
ORDER BY h_partition, h_account_id, h_region, h_instance_id
SKIP $offset LIMIT $limit_plus_one
MATCH (app:SSMHostApplication)
WHERE app.partition = h_partition
  AND app.account_id = h_account_id
  AND app.region = h_region
  AND app.instance_id = h_instance_id
  AND ($all_scopes OR app.scope_id IN $scope_ids)
OPTIONAL MATCH (app)-[rel:REPORTED_WITH]->(plat:SSMHostPlatform)
RETURN app AS app, plat AS plat`

// ssmHostView is one host's served inventory: the committed observations
// with the read-time status overlay.
type ssmHostView struct {
	Partition             string           `json:"partition"`
	AccountID             string           `json:"account_id"`
	Region                string           `json:"region"`
	InstanceID            string           `json:"instance_id"`
	Status                string           `json:"status"`
	StatusReason          string           `json:"status_reason"`
	CaptureTime           string           `json:"capture_time"`
	ScopeID               string           `json:"scope_id"`
	GenerationID          string           `json:"generation_id"`
	Applications          []map[string]any `json:"applications"`
	ApplicationsTruncated bool             `json:"applications_truncated"`
	Platforms             []map[string]any `json:"platforms"`
	FactsTruncated        bool             `json:"facts_truncated"`
	MalformedFacts        int              `json:"malformed_facts"`
}

// ssmHostInventoryReadModel is the paged host inventory read. MalformedFacts
// counts admitted facts the read skipped as undecodable; the reducer already
// quarantined each of them at projection time, and the read never fails for
// a malformed sibling.
type ssmHostInventoryReadModel struct {
	Hosts          []ssmHostView `json:"hosts"`
	MalformedFacts int           `json:"malformed_facts"`
	NextCursor     string        `json:"next_cursor"`
}

// ReadSSMHostInventory serves one SSM host inventory page: committed
// observation nodes from the graph overlaid with the active generation's
// admitted facts assessed through the same verdicts the reducer projects
// with. A host appears when committed nodes or active facts exist for it;
// anything else is absent, never an empty host.
func ReadSSMHostInventory(
	ctx context.Context,
	graph GraphQuery,
	store ssmHostFactsStore,
	filter ssmHostInventoryFilter,
	bounds ssmhost.Bounds,
	now time.Time,
) (ssmHostInventoryReadModel, error) {
	if err := bounds.Validate(); err != nil {
		return ssmHostInventoryReadModel{}, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = ssmHostInventoryDefaultLimit
	}
	committed, fetched, err := readSSMCommittedViews(ctx, graph, filter, limit)
	if err != nil {
		return ssmHostInventoryReadModel{}, err
	}
	active, err := store.SSMHostActiveFacts(ctx, filter)
	if err != nil {
		return ssmHostInventoryReadModel{}, err
	}
	observations, malformed := ssmObservationsFromFacts(active)
	projections, _ := ssmhost.ProjectHosts(observations.apps, observations.platforms, nil, bounds, now)
	verdicts := make(map[string]ssmhost.HostProjection, len(projections))
	for _, projection := range projections {
		verdicts[ssmhost.HostKey(projection.Host)] = projection
	}
	hosts := assembleSSMHostViews(committed, verdicts, filter, bounds, now)
	malformedTotal := 0
	for _, count := range malformed {
		malformedTotal += count
	}
	model := ssmHostInventoryReadModel{Hosts: hosts, MalformedFacts: malformedTotal}
	if fetched > limit {
		if len(model.Hosts) > limit {
			model.Hosts = model.Hosts[:limit]
		}
		model.NextCursor = strconv.Itoa(filter.Offset + limit)
	}
	return model, nil
}

type ssmCommittedHost struct {
	host       cloud.SSMHostTuple
	apps       []map[string]any
	platforms  map[string]map[string]any
	paired     map[string]bool
	scopeID    string
	generation string
}

func readSSMCommittedViews(ctx context.Context, graph GraphQuery, filter ssmHostInventoryFilter, limit int) (map[string]*ssmCommittedHost, int, error) {
	committed := map[string]*ssmCommittedHost{}
	if graph == nil {
		return committed, 0, nil
	}
	rows, err := graph.Run(ctx, ssmCommittedHostsCypher, map[string]any{
		"all_scopes":     filter.AllScopes,
		"scope_ids":      append([]string{}, append(filter.AllowedScopeIDs, filter.AllowedRepositoryIDs...)...),
		"instance_id":    filter.InstanceID,
		"account_id":     filter.AccountID,
		"region":         filter.Region,
		"offset":         filter.Offset,
		"limit_plus_one": limit + 1,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("query ssm committed host views: %w", err)
	}
	for _, row := range rows {
		app, _ := row["app"].(map[string]any)
		if app == nil {
			if plat, _ := row["plat_only"].(map[string]any); plat != nil {
				host := ssmCommittedHostFor(committed, plat)
				uid, _ := plat["uid"].(string)
				host.platforms[uid] = plat
			}
			continue
		}
		host := ssmCommittedHostFor(committed, app)
		host.apps = append(host.apps, app)
		if plat, _ := row["plat"].(map[string]any); plat != nil {
			uid, _ := plat["uid"].(string)
			host.platforms[uid] = plat
			if from, _ := app["uid"].(string); from != "" {
				host.paired[from+"->"+uid] = true
			}
		}
		if scope, _ := app["scope_id"].(string); scope != "" {
			host.scopeID = scope
		}
		if generation, _ := app["generation_id"].(string); generation != "" {
			host.generation = generation
		}
	}
	return committed, len(committed), nil
}

func ssmCommittedHostFor(committed map[string]*ssmCommittedHost, node map[string]any) *ssmCommittedHost {
	host := cloud.SSMHostTuple{
		Partition:  stringFromMap(node, "partition"),
		AccountID:  stringFromMap(node, "account_id"),
		Region:     stringFromMap(node, "region"),
		InstanceID: stringFromMap(node, "instance_id"),
	}
	key := ssmhost.HostKey(host)
	entry, ok := committed[key]
	if !ok {
		entry = &ssmCommittedHost{host: host, platforms: map[string]map[string]any{}, paired: map[string]bool{}}
		committed[key] = entry
	}
	return entry
}

type ssmFactObservations struct {
	apps      []ssmhost.ApplicationObservation
	platforms []ssmhost.PlatformObservation
}

func ssmObservationsFromFacts(active []ssmAdmittedFact) (ssmFactObservations, map[string]int) {
	var observations ssmFactObservations
	malformed := map[string]int{}
	for _, fact := range active {
		env := facts.Envelope{
			FactID:      fact.FactID,
			FactKind:    fact.FactKind,
			Payload:     fact.Payload,
			IsTombstone: fact.Tombstone,
		}
		switch fact.FactKind {
		case cloud.SSMHostApplicationFactKind:
			app, err := ssmhost.DecodeApplication(env)
			if err != nil {
				malformed[fact.ScopeID+"\x00"+fact.GenerationID]++
				continue
			}
			observations.apps = append(observations.apps, ssmhost.ApplicationObservation{Application: app, FactID: fact.FactID, Tombstone: fact.Tombstone})
		case cloud.SSMHostPlatformFactKind:
			platform, err := ssmhost.DecodePlatform(env)
			if err != nil {
				malformed[fact.ScopeID+"\x00"+fact.GenerationID]++
				continue
			}
			observations.platforms = append(observations.platforms, ssmhost.PlatformObservation{Platform: platform, FactID: fact.FactID, Tombstone: fact.Tombstone})
		}
	}
	return observations, malformed
}

func assembleSSMHostViews(
	committed map[string]*ssmCommittedHost,
	verdicts map[string]ssmhost.HostProjection,
	filter ssmHostInventoryFilter,
	bounds ssmhost.Bounds,
	now time.Time,
) []ssmHostView {
	keys := make([]string, 0, len(committed)+len(verdicts))
	seen := map[string]bool{}
	for key := range committed {
		keys = append(keys, key)
		seen[key] = true
	}
	for key := range verdicts {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var hosts []ssmHostView
	for _, key := range keys {
		entry := committed[key]
		projection, assessed := verdicts[key]
		if entry == nil && !assessed {
			continue
		}
		view := ssmHostViewFor(entry, projection, assessed, bounds, now)
		if filter.Status != "" && view.Status != filter.Status {
			continue
		}
		hosts = append(hosts, view)
	}
	return hosts
}

func ssmHostViewFor(
	entry *ssmCommittedHost,
	projection ssmhost.HostProjection,
	assessed bool,
	bounds ssmhost.Bounds,
	now time.Time,
) ssmHostView {
	view := ssmHostView{Status: ssmHostStatusUnknown, StatusReason: ssmHostReasonSilent}
	if entry != nil {
		view.Partition = entry.host.Partition
		view.AccountID = entry.host.AccountID
		view.Region = entry.host.Region
		view.InstanceID = entry.host.InstanceID
		view.ScopeID = entry.scopeID
		view.GenerationID = entry.generation
		view = viewWithCommittedRows(view, entry)
	}
	if !assessed {
		// No admitted facts for this host in the active generation: the
		// silent path. Committed truth stands; its age decides stale.
		if entry == nil {
			return view
		}
		if capture := committedAppCapture(entry); !capture.IsZero() && now.Sub(capture) > bounds.MaxCaptureAge {
			view.Status = ssmHostStatusStale
			view.StatusReason = ssmHostReasonStale
		}
		return view
	}
	view.Partition = projection.Host.Partition
	view.AccountID = projection.Host.AccountID
	view.Region = projection.Host.Region
	view.InstanceID = projection.Host.InstanceID
	if entry == nil {
		// Admitted but never projected: a host the reducer has not
		// materialized yet reads unknown with no rows, never an empty
		// host fabricated from facts alone.
		if !projection.AppVerdict.Absent() && !projection.AppVerdict.Complete && !projection.AppVerdict.Stale {
			view.Status = ssmHostStatusPartial
			view.StatusReason = ssmHostReasonPartial
			return view
		}
		view.StatusReason = ssmHostReasonPending
		return view
	}
	view.Status, view.StatusReason = overlayHostStatus(projection, entry, bounds, now)
	if view.Status == "" {
		// Every assessed stream withdrew its signal: the silent treatment.
		view.Status = ssmHostStatusUnknown
		view.StatusReason = ssmHostReasonSilent
		if capture := committedAppCapture(entry); !capture.IsZero() && now.Sub(capture) > bounds.MaxCaptureAge {
			view.Status = ssmHostStatusStale
			view.StatusReason = ssmHostReasonStale
		}
	}
	return view
}

// overlayHostStatus derives one host's status from the active generation's
// stream verdicts over the committed truth. Severity orders
// successful < stale < unknown < partial: a fresh negative signal always
// wins over age, and age always wins over a bare projection match.
func overlayHostStatus(projection ssmhost.HostProjection, entry *ssmCommittedHost, bounds ssmhost.Bounds, now time.Time) (string, string) {
	committedCapture := committedAppCapture(entry)
	appStatus, appReason := overlayAppStream(projection.AppVerdict, committedCapture, bounds, now)
	platStatus, platReason := overlayPlatformStreams(projection.PlatformVerdicts)
	if severity(appStatus) >= severity(platStatus) {
		return appStatus, appReason
	}
	return platStatus, platReason
}

func severity(status string) int {
	switch status {
	case "":
		return -1
	case ssmHostStatusSuccessful:
		return 0
	case ssmHostStatusStale:
		return 1
	case ssmHostStatusUnknown:
		return 2
	default:
		return 3
	}
}

func overlayAppStream(verdict cloud.SSMHostStreamAssessment, committedCapture time.Time, bounds ssmhost.Bounds, now time.Time) (string, string) {
	switch {
	case verdict.Absent():
		return "", ""
	case verdict.Stale:
		return ssmHostStatusStale, ssmHostReasonStale
	case verdict.Complete:
		switch {
		case committedCapture.IsZero() || verdict.CaptureTime.After(committedCapture):
			return ssmHostStatusUnknown, ssmHostReasonPending
		case now.Sub(committedCapture) > bounds.MaxCaptureAge:
			return ssmHostStatusStale, ssmHostReasonStale
		default:
			return ssmHostStatusSuccessful, ssmHostReasonProjected
		}
	default:
		return ssmHostStatusPartial, ssmHostReasonPartial
	}
}

func overlayPlatformStreams(verdicts map[string]cloud.SSMHostStreamAssessment) (string, string) {
	status, reason := "", ""
	for _, verdict := range verdicts {
		var candidate, candidateReason string
		switch {
		case verdict.Absent():
			continue
		case verdict.Stale:
			candidate, candidateReason = ssmHostStatusStale, ssmHostReasonStale
		case verdict.Complete:
			candidate, candidateReason = ssmHostStatusSuccessful, ssmHostReasonProjected
		default:
			candidate, candidateReason = ssmHostStatusPartial, ssmHostReasonPartial
		}
		if severity(candidate) >= severity(status) {
			status, reason = candidate, candidateReason
		}
	}
	return status, reason
}

// viewWithCommittedRows serves the committed observation rows with the
// allowlisted fields only. Anything the extractor did not allowlist never
// reaches the wire; the marshal-level test pins the absence of finding
// vocabulary.
func viewWithCommittedRows(view ssmHostView, entry *ssmCommittedHost) ssmHostView {
	sorted := append([]map[string]any{}, entry.apps...)
	sort.Slice(sorted, func(i, j int) bool {
		return stringFromMap(sorted[i], "uid") < stringFromMap(sorted[j], "uid")
	})
	for _, app := range sorted {
		if len(view.Applications) >= ssmHostInventoryMaxApplications {
			view.ApplicationsTruncated = true
			break
		}
		view.Applications = append(view.Applications, map[string]any{
			"uid":          app["uid"],
			"name":         app["name"],
			"version":      app["version"],
			"release":      app["release"],
			"epoch":        app["epoch"],
			"package_id":   app["package_id"],
			"architecture": app["architecture"],
			"capture_time": app["capture_time"],
		})
	}
	if capture := committedAppCapture(entry); !capture.IsZero() {
		view.CaptureTime = capture.UTC().Format(time.RFC3339)
	}
	var platforms []map[string]any
	for _, plat := range entry.platforms {
		platforms = append(platforms, map[string]any{
			"uid":              plat["uid"],
			"source_type":      plat["source_type"],
			"platform_type":    plat["platform_type"],
			"platform_name":    plat["platform_name"],
			"platform_version": plat["platform_version"],
			"capture_time":     plat["capture_time"],
			"observation_time": plat["observation_time"],
		})
	}
	sort.Slice(platforms, func(i, j int) bool {
		return stringFromMap(platforms[i], "uid") < stringFromMap(platforms[j], "uid")
	})
	view.Platforms = platforms
	return view
}

func committedAppCapture(entry *ssmCommittedHost) time.Time {
	var newest time.Time
	for _, app := range entry.apps {
		raw, _ := app["capture_time"].(string)
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		if parsed.After(newest) {
			newest = parsed
		}
	}
	return newest
}
