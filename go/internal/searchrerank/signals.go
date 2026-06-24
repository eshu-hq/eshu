// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchrerank

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// SignalKind names one code-to-cloud graph anchor a result can be reranked
// around. The set is intentionally small and grounded in the graph handle kinds
// curated search documents already carry.
type SignalKind string

const (
	// SignalService fires when a result is tied to a service-story anchor.
	SignalService SignalKind = "service_anchor"
	// SignalWorkload fires when a result is tied to a deployable workload unit.
	SignalWorkload SignalKind = "workload_anchor"
	// SignalDeployment fires when a result is tied to a runtime/deployment
	// summary.
	SignalDeployment SignalKind = "deployment"
	// SignalEnvironment fires when a result is tied to a deployment environment.
	SignalEnvironment SignalKind = "environment_anchor"
	// SignalIncident fires when a result is tied to an incident anchor.
	SignalIncident SignalKind = "incident"
	// SignalPackage fires when a result is tied to a package or container image,
	// the supply-chain anchor.
	SignalPackage SignalKind = "package"
	// SignalOwner fires when a result is tied to an ownership anchor.
	SignalOwner SignalKind = "owner"
)

// defaultWeights are the per-signal contribution weights. They order the
// anchors by how directly they tie a result to the bounded request scope: a
// matching service anchor is the strongest signal, ownership the weakest.
var defaultWeights = map[SignalKind]float64{
	SignalService:     1.0,
	SignalWorkload:    0.9,
	SignalIncident:    0.8,
	SignalDeployment:  0.7,
	SignalPackage:     0.6,
	SignalEnvironment: 0.5,
	SignalOwner:       0.4,
}

// anchorMatchBonus multiplies a signal weight when the result handle matches the
// exact id the request was scoped to (for example a service handle equal to the
// requested service id), so an in-scope anchor outranks a merely present one.
const anchorMatchBonus = 1.5

// HandleSignal maps a graph handle kind to the rerank signal it contributes to
// and whether that signal is anchored to a request scope id. It is exported so
// callers that synthesize follow-up calls from handles (such as the query
// layer) reuse the same mapping instead of duplicating it. The bool reports
// scope-anchoring, not whether the kind is known; an unknown kind returns an
// empty SignalKind.
func HandleSignal(kind string) (SignalKind, bool) {
	return handleSignal(kind)
}

// handleSignal maps a graph handle kind to the rerank signal it contributes to,
// and whether that signal is anchored to a request scope id. Unknown handle
// kinds contribute no signal.
func handleSignal(kind string) (SignalKind, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "service":
		return SignalService, true
	case "workload":
		return SignalWorkload, true
	case "runtime_summary", "deployment":
		return SignalDeployment, false
	case "environment":
		return SignalEnvironment, true
	case "incident":
		return SignalIncident, false
	case "container_image", "package", "image":
		return SignalPackage, false
	case "owner", "team", "ownership":
		return SignalOwner, false
	default:
		return "", false
	}
}

// scopeAnchorID returns the request scope id a signal is anchored to, if any.
func scopeAnchorID(signal SignalKind, scope searchretrieval.Scope) (string, bool) {
	switch signal {
	case SignalService:
		return strings.TrimSpace(scope.ServiceID), strings.TrimSpace(scope.ServiceID) != ""
	case SignalWorkload:
		return strings.TrimSpace(scope.WorkloadID), strings.TrimSpace(scope.WorkloadID) != ""
	case SignalEnvironment:
		return strings.TrimSpace(scope.Environment), strings.TrimSpace(scope.Environment) != ""
	default:
		return "", false
	}
}

// resultSignals is the graph-signal summary for one result: the total boost and
// the per-signal contributions, strongest first.
type resultSignals struct {
	boost         float64
	contributions []Contribution
}

// extractSignals scores one result's graph proximity from its own handles. Each
// signal kind contributes at most once, keeping the strongest matching handle.
// A handle whose id matches the request scope anchor earns the anchor-match
// bonus; presence-only handles still contribute their base weight.
func extractSignals(
	result searchretrieval.Result,
	scope searchretrieval.Scope,
	weights map[SignalKind]float64,
) resultSignals {
	best := make(map[SignalKind]Contribution)
	for _, h := range result.Handles {
		signal, anchored := handleSignal(h.Kind)
		if signal == "" {
			continue
		}
		weight := weights[signal]
		if weight <= 0 {
			continue
		}
		if anchored {
			// scopeAnchorID returns ok only when the request actually scoped to
			// this kind. When it did, an exact id match earns the anchor bonus and
			// a mismatch is skipped as out-of-scope. When the request did not scope
			// to this kind (ok is false), a present handle is still a real
			// proximity signal, so it falls through and earns its base weight.
			if want, ok := scopeAnchorID(signal, scope); ok {
				if strings.TrimSpace(h.ID) != want {
					continue
				}
				weight *= anchorMatchBonus
			}
		}
		contribution := Contribution{
			Kind:   signal,
			Handle: handleKey(strings.ToLower(strings.TrimSpace(h.Kind)), strings.TrimSpace(h.ID)),
			Weight: weight,
		}
		if existing, ok := best[signal]; !ok || weight > existing.Weight {
			best[signal] = contribution
		}
	}

	signals := resultSignals{contributions: make([]Contribution, 0, len(best))}
	for _, contribution := range best {
		signals.boost += contribution.Weight
		signals.contributions = append(signals.contributions, contribution)
	}
	sort.SliceStable(signals.contributions, func(i, j int) bool {
		if signals.contributions[i].Weight != signals.contributions[j].Weight {
			return signals.contributions[i].Weight > signals.contributions[j].Weight
		}
		return signals.contributions[i].Kind < signals.contributions[j].Kind
	})
	if len(signals.contributions) == 0 {
		signals.contributions = nil
	}
	return signals
}

// mergedWeights overlays caller weight overrides on the defaults. A nil map
// yields the defaults; an explicit non-positive override disables that signal.
func mergedWeights(overrides map[SignalKind]float64) map[SignalKind]float64 {
	merged := make(map[SignalKind]float64, len(defaultWeights))
	for kind, weight := range defaultWeights {
		merged[kind] = weight
	}
	for kind, weight := range overrides {
		merged[kind] = weight
	}
	return merged
}
