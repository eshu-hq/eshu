package reducer

import (
	"strings"

	"golang.org/x/mod/semver"
)

type supplyChainAffectedRange struct {
	kind   string
	events []supplyChainAffectedRangeEvent
}

type supplyChainAffectedRangeEvent struct {
	introduced   string
	fixed        string
	lastAffected string
	limit        string
}

func supplyChainAffectedRangesFromPayload(payload map[string]any) []supplyChainAffectedRange {
	rawRanges, ok := payload["affected_ranges"].([]any)
	if !ok {
		return nil
	}
	ranges := make([]supplyChainAffectedRange, 0, len(rawRanges))
	for _, rawRange := range rawRanges {
		rangeMap, ok := rawRange.(map[string]any)
		if !ok {
			continue
		}
		item := supplyChainAffectedRange{
			kind:   payloadStr(rangeMap, "type"),
			events: supplyChainAffectedRangeEvents(rangeMap["events"]),
		}
		if item.kind != "" && len(item.events) > 0 {
			ranges = append(ranges, item)
		}
	}
	return ranges
}

func supplyChainAffectedRangeEvents(raw any) []supplyChainAffectedRangeEvent {
	rawEvents, ok := raw.([]any)
	if !ok {
		return nil
	}
	events := make([]supplyChainAffectedRangeEvent, 0, len(rawEvents))
	for _, rawEvent := range rawEvents {
		eventMap, ok := rawEvent.(map[string]any)
		if !ok {
			continue
		}
		event := supplyChainAffectedRangeEvent{
			introduced:   payloadStr(eventMap, "introduced"),
			fixed:        payloadStr(eventMap, "fixed"),
			lastAffected: payloadStr(eventMap, "last_affected"),
			limit:        payloadStr(eventMap, "limit"),
		}
		if event.introduced != "" || event.fixed != "" ||
			event.lastAffected != "" || event.limit != "" {
			events = append(events, event)
		}
	}
	return events
}

func semverRangeContainsDecision(
	affectedRange supplyChainAffectedRange,
	observed string,
) (bool, bool) {
	if ok, valid := semverBeforeLimitsDecision(observed, affectedRange.events); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	vulnerable := false
	for _, event := range affectedRange.events {
		switch {
		case event.introduced != "":
			if ok, valid := semverAtLeast(observed, event.introduced); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.fixed != "":
			if ok, valid := semverAtLeast(observed, event.fixed); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.lastAffected != "":
			if ok, valid := semverGreaterThan(observed, event.lastAffected); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

func semverBeforeLimitsDecision(observed string, events []supplyChainAffectedRangeEvent) (bool, bool) {
	hasLimit := false
	for _, event := range events {
		limit := strings.TrimSpace(event.limit)
		if limit == "" {
			continue
		}
		hasLimit = true
		if limit == "*" {
			return true, true
		}
		if ok, valid := semverLessThan(observed, limit); !valid {
			return false, false
		} else if ok {
			return true, true
		}
	}
	return !hasLimit, true
}

func semverAtLeast(left string, right string) (bool, bool) {
	cmp, ok := compareOSVSemver(left, right)
	return cmp >= 0, ok
}

func semverGreaterThan(left string, right string) (bool, bool) {
	cmp, ok := compareOSVSemver(left, right)
	return cmp > 0, ok
}

func semverLessThan(left string, right string) (bool, bool) {
	cmp, ok := compareOSVSemver(left, right)
	return cmp < 0, ok
}

func compareOSVSemver(left string, right string) (int, bool) {
	leftNormalized, ok := normalizeOSVSemver(left)
	if !ok {
		return 0, false
	}
	rightNormalized, ok := normalizeOSVSemver(right)
	if !ok {
		return 0, false
	}
	return semver.Compare(leftNormalized, rightNormalized), true
}

func normalizeOSVSemver(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if raw == "0" {
		return "v0.0.0", true
	}
	if !strings.HasPrefix(raw, "v") {
		raw = "v" + raw
	}
	if !semver.IsValid(raw) {
		return "", false
	}
	return raw, true
}

func exactManifestDependencyVersion(raw string) (string, bool) {
	version := strings.TrimSpace(raw)
	if version == "" {
		return "", false
	}
	lower := strings.ToLower(version)
	if lower == "latest" || nonVersionDependencyPrefix(lower) {
		return "", false
	}
	if strings.ContainsAny(version, "<>^~*=|, []") ||
		strings.Contains(lower, " - ") ||
		strings.Contains(version, "$(") ||
		strings.Contains(lower, ".x") ||
		strings.Contains(lower, "x.") {
		return "", false
	}
	return version, true
}

func nonVersionDependencyPrefix(lower string) bool {
	for _, prefix := range []string{
		"file:",
		"git+",
		"github:",
		"http:",
		"https:",
		"link:",
		"npm:",
		"portal:",
		"workspace:",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
