// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssmhost

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// ReportedWithRelationship is the closed single-member relationship
// vocabulary this projection writes: one application observation row reported
// with the platform evidence admitted for its host in the same generation
// within the cross-type skew bound.
const ReportedWithRelationship = "REPORTED_WITH"

// Bounds carries the freshness configuration one SSM host projection is
// assessed under. Every field is required: missing limits fail the handler
// closed at construction, mirroring the collector's explicit-limit rule
// (eshu-hq/eshu#9556). The skew bound pairs application and platform evidence
// AWS never promises at equal capture times; the platform observation age
// governs DescribeInstanceInformation evidence, which has no inventory
// capture time.
type Bounds struct {
	MaxCaptureAge             time.Duration
	MaxCrossTypeSkew          time.Duration
	MaxPlatformObservationAge time.Duration
}

// Validate fails closed unless every bound is positive and the skew bound
// sits within the capture-age bound (a wider skew admits pairs no single
// capture could be fresh for).
func (b Bounds) Validate() error {
	if b.MaxCaptureAge <= 0 {
		return fmt.Errorf("ssm host projection: max capture age must be positive")
	}
	if b.MaxCrossTypeSkew <= 0 {
		return fmt.Errorf("ssm host projection: max cross-type skew must be positive")
	}
	if b.MaxCrossTypeSkew > b.MaxCaptureAge {
		return fmt.Errorf("ssm host projection: max cross-type skew %v exceeds max capture age %v", b.MaxCrossTypeSkew, b.MaxCaptureAge)
	}
	if b.MaxPlatformObservationAge <= 0 {
		return fmt.Errorf("ssm host projection: max platform observation age must be positive")
	}
	return nil
}

// HostKey encodes one EC2 managed-node observation boundary for grouping.
// The NUL separator cannot appear in a validated tuple field, so distinct
// tuples never collide and one host's rows never merge into another's.
func HostKey(host cloud.SSMHostTuple) string {
	return strings.Join([]string{host.Partition, host.AccountID, host.Region, host.InstanceID}, "\x00")
}

// ApplicationObservation pairs a decoded application row with the envelope
// provenance the row builder stamps onto the graph row.
type ApplicationObservation struct {
	Application
	FactID         string
	Tombstone      bool
	SourceOrderKey string
}

// PlatformObservation pairs a decoded platform observation with the envelope
// provenance the row builder stamps onto the graph row.
type PlatformObservation struct {
	Platform
	FactID         string
	Tombstone      bool
	SourceOrderKey string
}

// ExtractObservations decodes admitted SSM fact envelopes into typed
// observations. A malformed fact is quarantined per fact (input_invalid)
// while every valid fact still projects; a non-decode failure aborts the
// batch so the durable queue triages it.
func ExtractObservations(appEnvelopes, platformEnvelopes []facts.Envelope) ([]ApplicationObservation, []PlatformObservation, []factdecode.QuarantinedFact, error) {
	var apps []ApplicationObservation
	var platforms []PlatformObservation
	var quarantined []factdecode.QuarantinedFact
	for _, env := range appEnvelopes {
		if env.FactKind != cloud.SSMHostApplicationFactKind {
			continue
		}
		app, err := DecodeApplication(env)
		if err != nil {
			quarantined, err = partitionFailure(quarantined, env, err)
			if err != nil {
				return nil, nil, nil, err
			}
			continue
		}
		apps = append(apps, ApplicationObservation{
			Application:    app,
			FactID:         env.FactID,
			Tombstone:      env.IsTombstone,
			SourceOrderKey: payloadcore.SourceOrderKey(env),
		})
	}
	for _, env := range platformEnvelopes {
		if env.FactKind != cloud.SSMHostPlatformFactKind {
			continue
		}
		platform, err := DecodePlatform(env)
		if err != nil {
			quarantined, err = partitionFailure(quarantined, env, err)
			if err != nil {
				return nil, nil, nil, err
			}
			continue
		}
		platforms = append(platforms, PlatformObservation{
			Platform:       platform,
			FactID:         env.FactID,
			Tombstone:      env.IsTombstone,
			SourceOrderKey: payloadcore.SourceOrderKey(env),
		})
	}
	return apps, platforms, quarantined, nil
}

func partitionFailure(quarantined []factdecode.QuarantinedFact, env facts.Envelope, err error) ([]factdecode.QuarantinedFact, error) {
	quarantinedFact, ok, fatal := factdecode.PartitionDecodeFailures(env, err)
	if fatal != nil {
		return nil, fatal
	}
	if ok {
		quarantined = append(quarantined, quarantinedFact)
	}
	return quarantined, nil
}

// CommittedHost is the projection the graph already holds for one host: the
// newest committed application capture time. The writer reads it back so a
// delayed or duplicated older capture can never supersede (and retract)
// newer truth. Platform evidence carries no committed comparison, so no
// per-provenance platform time is tracked.
type CommittedHost struct {
	AppCapture    time.Time
	HasAppCapture bool
}

// Skip reasons for the bounded completion-log tally. A tombstoned fact
// withdraws its observation; it is counted, never silently dropped.
const skipTombstone = "tombstone"

// HostProjection is one host's assessed projection: the stream verdicts, the
// supersession decision, and the deterministic graph rows for a superseding
// host (empty otherwise — a preserved host's prior graph state stands
// untouched).
type HostProjection struct {
	Host             cloud.SSMHostTuple
	AppVerdict       cloud.SSMHostStreamAssessment
	PlatformVerdicts map[string]cloud.SSMHostStreamAssessment
	// ConflictedPlatformSources names the provenances whose current-generation
	// evidence conflicts: on supersession their prior nodes retract with no
	// replacement, so stale agreed truth never stands beside an unresolved
	// conflict.
	ConflictedPlatformSources []string
	Supersede                 bool
	Reason                    string
	AppRows                   []map[string]any
	PlatformRows              []map[string]any
	EdgeRows                  []map[string]any
	Skipped                   map[string]int
}

// ProjectionTally counts host verdicts for one generation so the handler can
// emit the bounded completion log without a per-host line.
type ProjectionTally struct {
	Hosts      int
	Superseded int
	Preserved  int
	Skipped    map[string]int
}

// ProjectHosts assesses one generation's observations per host and builds
// deterministic graph rows for the superseding hosts. Only a fresh complete
// application capture newer than the committed one supersedes: mixed captures,
// aged captures, and delayed duplicates preserve the prior generation with
// the verdict carried on the projection for the handler's evidence summary.
// Conflicting platform content for one host and provenance stays ambiguous
// (no platform row, no edge) without blocking the coherent application
// capture; an out-of-skew platform still projects, unpaired.
func ProjectHosts(
	apps []ApplicationObservation,
	platforms []PlatformObservation,
	committed map[string]CommittedHost,
	bounds Bounds,
	now time.Time,
) ([]HostProjection, ProjectionTally) {
	tally := ProjectionTally{Skipped: map[string]int{}}
	byHost := map[string]*hostWork{}
	for _, app := range apps {
		work := workFor(byHost, app.HostTuple())
		if app.Tombstone {
			work.skipped[skipTombstone]++
			tally.Skipped[skipTombstone]++
			continue
		}
		work.apps = append(work.apps, app)
	}
	for _, platform := range platforms {
		work := workFor(byHost, platform.HostTuple())
		if platform.Tombstone {
			work.skipped[skipTombstone]++
			tally.Skipped[skipTombstone]++
			continue
		}
		work.platforms = append(work.platforms, platform)
	}

	keys := make([]string, 0, len(byHost))
	for key := range byHost {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	projections := make([]HostProjection, 0, len(keys))
	for _, key := range keys {
		work := byHost[key]
		projection := projectOneHost(work, committed[key], bounds, now)
		projections = append(projections, projection)
		tally.Hosts++
		if projection.Supersede {
			tally.Superseded++
		} else {
			tally.Preserved++
		}
	}
	return projections, tally
}

type hostWork struct {
	host      cloud.SSMHostTuple
	apps      []ApplicationObservation
	platforms []PlatformObservation
	skipped   map[string]int
}

func workFor(byHost map[string]*hostWork, host cloud.SSMHostTuple) *hostWork {
	key := HostKey(host)
	work, ok := byHost[key]
	if !ok {
		work = &hostWork{host: host, skipped: map[string]int{}}
		byHost[key] = work
	}
	return work
}

func projectOneHost(work *hostWork, prior CommittedHost, bounds Bounds, now time.Time) HostProjection {
	projection := HostProjection{
		Host:             work.host,
		PlatformVerdicts: map[string]cloud.SSMHostStreamAssessment{},
		Skipped:          work.skipped,
	}
	appVerdict := cloud.AssessSSMHostApplicationStream(applicationViewRows(work.apps), now, bounds.MaxCaptureAge)
	projection.AppVerdict = appVerdict
	platformBySource := groupPlatformsBySource(work.platforms)
	sources := make([]string, 0, len(platformBySource))
	for source := range platformBySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	pairedSources := map[string]bool{}
	for _, source := range sources {
		if platformContentConflict(platformBySource[source]) {
			projection.ConflictedPlatformSources = append(projection.ConflictedPlatformSources, source)
			projection.PlatformVerdicts[source] = cloud.SSMHostStreamAssessment{Reason: "conflicting platform observations stay unresolved"}
			continue
		}
		verdict := assessPlatformSource(platformBySource[source], now, bounds)
		projection.PlatformVerdicts[source] = verdict
		if !verdict.Complete || !appVerdict.Complete {
			continue
		}
		if cloud.SSMSkewPairs(appVerdict.CaptureTime, verdict.CaptureTime, bounds.MaxCrossTypeSkew) {
			pairedSources[source] = true
		}
	}
	if !appVerdict.Complete {
		projection.Reason = "application stream not superseding: " + appVerdict.Reason
		return projection
	}
	if prior.HasAppCapture && !appVerdict.CaptureTime.After(prior.AppCapture) {
		projection.Reason = "capture not newer than the committed capture: delayed duplicate preserves prior truth"
		return projection
	}
	projection.Supersede = true
	projection.Reason = "complete capture supersedes prior truth"
	projection.AppRows = buildApplicationRows(work.apps)
	for _, source := range sources {
		if platformContentConflict(platformBySource[source]) {
			continue
		}
		projection.PlatformRows = append(projection.PlatformRows, buildPlatformRow(newestPlatform(platformBySource[source])))
	}
	projection.EdgeRows = buildPairEdges(projection.AppRows, projection.PlatformRows, pairedSources)
	return projection
}

func applicationViewRows(apps []ApplicationObservation) []cloud.SSMHostApplicationViewRow {
	rows := make([]cloud.SSMHostApplicationViewRow, 0, len(apps))
	for _, app := range apps {
		rows = append(rows, cloud.SSMHostApplicationViewRow{
			Host:        app.HostTuple(),
			CaptureTime: app.CaptureTime,
			RowHash:     CanonicalApplicationRowHash(app.Application),
		})
	}
	return rows
}

func groupPlatformsBySource(platforms []PlatformObservation) map[string][]PlatformObservation {
	grouped := map[string][]PlatformObservation{}
	for _, platform := range platforms {
		grouped[platform.SourceType] = append(grouped[platform.SourceType], platform)
	}
	return grouped
}

// platformContentConflict reports whether one host and provenance carries
// conflicting platform content in one generation. Identical observations are
// duplicate delivery; differing platform fields are ambiguous and stay
// unresolved — never settled by ordering.
func platformContentConflict(platforms []PlatformObservation) bool {
	first := platforms[0]
	for _, other := range platforms[1:] {
		if deref(other.PlatformType) != deref(first.PlatformType) ||
			deref(other.PlatformName) != deref(first.PlatformName) ||
			deref(other.PlatformVersion) != deref(first.PlatformVersion) {
			return true
		}
	}
	return false
}

// newestPlatform resolves duplicate platform delivery by the #5007
// max-source-order rule: the latest observation wins the row, independent of
// slice order or worker commit order. It never resolves conflicting content;
// callers check platformContentConflict first.
func newestPlatform(platforms []PlatformObservation) PlatformObservation {
	winner := platforms[0]
	for _, other := range platforms[1:] {
		if other.SourceOrderKey > winner.SourceOrderKey {
			winner = other
		}
	}
	return winner
}

// assessPlatformSource verdicts one conflict-free provenance stream. Callers
// check platformContentConflict first; conflicting content never reaches this
// assessment.
func assessPlatformSource(platforms []PlatformObservation, now time.Time, bounds Bounds) cloud.SSMHostStreamAssessment {
	rows := make([]cloud.SSMHostPlatformViewRow, 0, len(platforms))
	for _, platform := range platforms {
		rows = append(rows, cloud.SSMHostPlatformViewRow{
			Host:            platform.HostTuple(),
			SourceType:      platform.SourceType,
			CaptureTime:     deref(platform.CaptureTime),
			ObservationTime: platform.ObservationTime,
		})
	}
	return cloud.AssessSSMHostPlatformStream(rows, now, bounds.MaxCaptureAge, bounds.MaxPlatformObservationAge)
}

func buildApplicationRows(apps []ApplicationObservation) []map[string]any {
	byUID := make(map[string]map[string]any, len(apps))
	for _, app := range apps {
		row := map[string]any{
			"uid":                           ApplicationStableKey(app.Application),
			"partition":                     app.Partition,
			"account_id":                    app.AccountID,
			"region":                        app.Region,
			"instance_id":                   app.InstanceID,
			"resource_type":                 app.ResourceType,
			"source_type":                   app.SourceType,
			"schema_version":                app.SchemaVersion,
			"capture_time":                  app.CaptureTime,
			"name":                          nilString(app.Name),
			"version":                       nilString(app.Version),
			"release":                       nilString(app.Release),
			"epoch":                         nilString(app.Epoch),
			"package_id":                    nilString(app.PackageID),
			"architecture":                  nilString(app.Architecture),
			"source_fact_id":                app.FactID,
			payloadcore.SourceOrderKeyField: app.SourceOrderKey,
		}
		if payloadcore.PreferMaxSourceOrderKey(byUID[row["uid"].(string)], row) {
			byUID[row["uid"].(string)] = row
		}
	}
	return sortedRows(byUID)
}

func buildPlatformRow(winner PlatformObservation) map[string]any {
	return map[string]any{
		"uid":                           PlatformStableKey(winner.Platform),
		"partition":                     winner.Partition,
		"account_id":                    winner.AccountID,
		"region":                        winner.Region,
		"instance_id":                   winner.InstanceID,
		"resource_type":                 winner.ResourceType,
		"source_type":                   winner.SourceType,
		"platform_type":                 nilString(winner.PlatformType),
		"platform_name":                 nilString(winner.PlatformName),
		"platform_version":              nilString(winner.PlatformVersion),
		"capture_time":                  nilString(winner.CaptureTime),
		"observation_time":              winner.ObservationTime,
		"source_fact_id":                winner.FactID,
		payloadcore.SourceOrderKeyField: winner.SourceOrderKey,
	}
}

func buildPairEdges(appRows, platformRows []map[string]any, pairedSources map[string]bool) []map[string]any {
	bySource := map[string]string{}
	for _, row := range platformRows {
		if source, ok := row["source_type"].(string); ok {
			bySource[source] = row["uid"].(string)
		}
	}
	sources := make([]string, 0, len(bySource))
	for source := range bySource {
		if pairedSources[source] {
			sources = append(sources, source)
		}
	}
	sort.Strings(sources)
	var edges []map[string]any
	for _, row := range appRows {
		from, _ := row["uid"].(string)
		for _, source := range sources {
			edges = append(edges, map[string]any{
				"from_uid":     from,
				"to_uid":       bySource[source],
				"relationship": ReportedWithRelationship,
			})
		}
	}
	return edges
}

func sortedRows(byUID map[string]map[string]any) []map[string]any {
	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	rows := make([]map[string]any, 0, len(uids))
	for _, uid := range uids {
		rows = append(rows, byUID[uid])
	}
	return rows
}

func nilString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
