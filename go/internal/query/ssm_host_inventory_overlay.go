// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

type ssmFactObservations struct {
	apps      []ssmhost.ApplicationObservation
	platforms []ssmhost.PlatformObservation
}

// ssmHostReadSignals carries the per-host read inputs the verdict overlay
// needs beyond the assessed projections: which hosts exhausted the
// admitted-fact window, and how many malformed facts each host carries.
type ssmHostReadSignals struct {
	malformedByHost map[string]int
	truncatedHosts  map[string]bool
}

// ssmAdmittedHostKey groups raw admitted rows by host tuple exactly the way
// the SQL window partitions them (payload partition, account, region,
// instance), so the Go-side per-host counts mirror the window slot-for-slot,
// including rows that later fail decode. The encoding matches ssmhost.HostKey,
// so signal keys align with the verdict and committed maps.
func ssmAdmittedHostKey(payload map[string]any) string {
	return strings.Join([]string{
		stringFromMap(payload, "partition"),
		stringFromMap(payload, "account_id"),
		stringFromMap(payload, "region"),
		stringFromMap(payload, "instance_id"),
	}, "\x00")
}

func ssmObservationsFromFacts(active []ssmAdmittedFact) (ssmFactObservations, ssmHostReadSignals) {
	var observations ssmFactObservations
	signals := ssmHostReadSignals{
		malformedByHost: map[string]int{},
		truncatedHosts:  map[string]bool{},
	}
	byHost := map[string][]ssmAdmittedFact{}
	var hostOrder []string
	for _, fact := range active {
		key := ssmAdmittedHostKey(fact.Payload)
		if _, ok := byHost[key]; !ok {
			hostOrder = append(hostOrder, key)
		}
		byHost[key] = append(byHost[key], fact)
	}
	for _, key := range hostOrder {
		rows := byHost[key]
		if len(rows) > ssmHostInventoryMaxFactRowsPerHost {
			// The SQL window keeps one row past the cap; filling it
			// means the stream was short-read. Assess the capped
			// prefix but force unknown downstream, never a coherence
			// verdict on the truncated stream.
			signals.truncatedHosts[key] = true
			rows = rows[:ssmHostInventoryMaxFactRowsPerHost]
		}
		for _, fact := range rows {
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
					signals.malformedByHost[key]++
					continue
				}
				observations.apps = append(observations.apps, ssmhost.ApplicationObservation{Application: app, FactID: fact.FactID, Tombstone: fact.Tombstone})
			case cloud.SSMHostPlatformFactKind:
				platform, err := ssmhost.DecodePlatform(env)
				if err != nil {
					signals.malformedByHost[key]++
					continue
				}
				observations.platforms = append(observations.platforms, ssmhost.PlatformObservation{Platform: platform, FactID: fact.FactID, Tombstone: fact.Tombstone})
			}
		}
	}
	return observations, signals
}

// assembleSSMHostViews builds the views for exactly the page keys: the
// committed entry (nil for active-only hosts) overlaid with the assessed
// verdict (unassessed when the host has no active facts). Status filters
// apply after paging, so a filtered page may be short.
func assembleSSMHostViews(
	committed map[string]*ssmCommittedHost,
	verdicts map[string]ssmhost.HostProjection,
	signals ssmHostReadSignals,
	page []string,
	filter ssmHostInventoryFilter,
	bounds ssmhost.Bounds,
	now time.Time,
) []ssmHostView {
	var hosts []ssmHostView
	for _, key := range page {
		entry := committed[key]
		projection, assessed := verdicts[key]
		if entry == nil && !assessed {
			continue
		}
		view := ssmHostViewFor(key, entry, projection, assessed, signals, bounds, now)
		if filter.Status != "" && view.Status != filter.Status {
			continue
		}
		hosts = append(hosts, view)
	}
	return hosts
}

// ssmHostViewFor builds one host's view and applies the read-signal overlay:
// per-host malformed counts always land on the view, and a host that
// exhausted the admitted-fact window reads unknown with FactsTruncated set,
// never a coherence verdict on the truncated stream.
func ssmHostViewFor(
	key string,
	entry *ssmCommittedHost,
	projection ssmhost.HostProjection,
	assessed bool,
	signals ssmHostReadSignals,
	bounds ssmhost.Bounds,
	now time.Time,
) ssmHostView {
	view := ssmHostViewForAssessed(entry, projection, assessed, bounds, now)
	view.MalformedFacts = signals.malformedByHost[key]
	if signals.truncatedHosts[key] {
		view.FactsTruncated = true
		view.Status = ssmHostStatusUnknown
		view.StatusReason = ssmHostReasonTruncatedFacts
	}
	return view
}

func ssmHostViewForAssessed(
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
