package goldenaudit

import (
	"fmt"
	"sort"
)

// Score captures precision and recall for one relationship type or the overall
// roll-up. Precision is correct observed edges over total observed edges; recall
// is correct observed edges over total golden edges.
//
// When a denominator is zero the convention is:
//   - precision = 1.0 when there are also no golden edges in scope, else 0.0,
//   - recall = 1.0 when there are also no observed edges in scope, else 0.0.
//
// This keeps an empty-vs-empty comparison a perfect score while still failing a
// scorer that emits nothing against a non-empty golden set, or that emits edges
// against an empty golden set.
type Score struct {
	Precision     float64
	Recall        float64
	CorrectEdges  int
	ObservedEdges int
	GoldenEdges   int
}

// TypeAccuracy is the precision/recall score for a single relationship type.
type TypeAccuracy struct {
	Type string
	Score
}

// AccuracyResult reports per-relationship-type and overall precision/recall of
// observed call/reference edges against golden edges, plus a debuggable
// mismatch breakdown.
//
// An observed edge is correct iff a golden edge exists with the same
// (SourceID, Type, TargetID) identity, i.e. the same Edge.Key(). The mismatch
// lists classify every non-correct edge by (SourceID, Type) grouping:
//
//   - WrongTarget: observed edges whose (SourceID, Type) matches at least one
//     golden edge, but whose TargetID matches none of that group's golden
//     targets. These are the dangerous "resolved to the wrong callee" edges
//     that tier distribution alone cannot catch.
//   - Extra: observed edges whose (SourceID, Type) matches no golden edge.
//   - Missing: golden edges with no correct observed match.
//
// All edge lists are sorted by Edge.Key() so failures are deterministic.
type AccuracyResult struct {
	Overall Score
	ByType  []TypeAccuracy

	WrongTarget []Edge
	Missing     []Edge
	Extra       []Edge
}

// Pass reports whether observed edges exactly reproduce golden edges, i.e. the
// mismatch breakdown is empty.
func (r AccuracyResult) Pass() bool {
	return len(r.WrongTarget) == 0 && len(r.Missing) == 0 && len(r.Extra) == 0
}

// Summary returns a stable one-line accuracy summary for test failures.
func (r AccuracyResult) Summary() string {
	return fmt.Sprintf(
		"precision=%.3f recall=%.3f correct=%d observed=%d golden=%d wrong_target=%d missing=%d extra=%d",
		r.Overall.Precision, r.Overall.Recall,
		r.Overall.CorrectEdges, r.Overall.ObservedEdges, r.Overall.GoldenEdges,
		len(r.WrongTarget), len(r.Missing), len(r.Extra),
	)
}

// ScoreAccuracy computes precision/recall of observed edges against golden
// edges, per relationship type and overall, with a wrong-target vs missing vs
// extra breakdown.
//
// Edge identity reuses Edge.Key() ((SourceID, Type, TargetID)); the
// wrong-target vs extra distinction reuses the (SourceID, Type) prefix of that
// same identity. Duplicate edges (same Key) are collapsed so repeated emission
// does not inflate counts. ScoreAccuracy performs measurement only and never
// mutates either graph.
func ScoreAccuracy(golden Graph, observed Graph) AccuracyResult {
	goldenKeys := edgeKeySet(golden.Edges)
	observedKeys := edgeKeySet(observed.Edges)
	goldenSourceTargets := sourceTypeTargets(golden.Edges)

	perType := make(map[string]*typeTally)
	tallyOf := func(relType string) *typeTally {
		t, ok := perType[relType]
		if !ok {
			t = &typeTally{}
			perType[relType] = t
		}
		return t
	}

	var wrongTarget, extra, missing []Edge

	// Classify each unique observed edge as correct, wrong-target, or extra.
	for key, edge := range observedKeys {
		tally := tallyOf(edge.Type)
		tally.observed++
		if _, ok := goldenKeys[key]; ok {
			tally.correct++
			continue
		}
		if _, ok := goldenSourceTargets[sourceTypeKey(edge)]; ok {
			wrongTarget = append(wrongTarget, edge)
		} else {
			extra = append(extra, edge)
		}
	}

	// Count golden totals and collect golden edges with no correct observed match.
	for key, edge := range goldenKeys {
		tally := tallyOf(edge.Type)
		tally.golden++
		if _, ok := observedKeys[key]; !ok {
			missing = append(missing, edge)
		}
	}

	sortEdges(wrongTarget)
	sortEdges(extra)
	sortEdges(missing)

	result := AccuracyResult{
		ByType:      make([]TypeAccuracy, 0, len(perType)),
		WrongTarget: wrongTarget,
		Missing:     missing,
		Extra:       extra,
	}

	var overall typeTally
	relTypes := make([]string, 0, len(perType))
	for relType := range perType {
		relTypes = append(relTypes, relType)
	}
	sort.Strings(relTypes)
	for _, relType := range relTypes {
		tally := perType[relType]
		overall.correct += tally.correct
		overall.observed += tally.observed
		overall.golden += tally.golden
		result.ByType = append(result.ByType, TypeAccuracy{
			Type:  relType,
			Score: tally.score(),
		})
	}
	result.Overall = overall.score()
	return result
}

// typeTally accumulates correct/observed/golden edge counts for one
// relationship type before they are turned into a Score.
type typeTally struct {
	correct  int
	observed int
	golden   int
}

func (t typeTally) score() Score {
	return Score{
		Precision:     ratio(t.correct, t.observed, t.golden == 0),
		Recall:        ratio(t.correct, t.golden, t.observed == 0),
		CorrectEdges:  t.correct,
		ObservedEdges: t.observed,
		GoldenEdges:   t.golden,
	}
}

// ratio divides numerator by denominator, applying the div-by-zero convention:
// an empty denominator scores 1.0 when emptyCounterpart is true (nothing was
// expected in the counterpart dimension either) and 0.0 otherwise.
func ratio(numerator int, denominator int, emptyCounterpart bool) float64 {
	if denominator == 0 {
		if emptyCounterpart {
			return 1.0
		}
		return 0.0
	}
	return float64(numerator) / float64(denominator)
}

// edgeKeySet collapses edges by Edge.Key() so duplicates do not inflate counts.
func edgeKeySet(edges []Edge) map[string]Edge {
	set := make(map[string]Edge, len(edges))
	for _, edge := range edges {
		set[edge.Key()] = edge
	}
	return set
}

// sourceTypeKey is the (SourceID, Type) identity prefix used to separate
// wrong-target edges (a golden edge shares the source+type) from extra edges.
func sourceTypeKey(e Edge) string {
	return e.SourceID + "|" + e.Type
}

// sourceTypeTargets indexes the set of golden target IDs per (SourceID, Type).
func sourceTypeTargets(edges []Edge) map[string]map[string]struct{} {
	index := make(map[string]map[string]struct{}, len(edges))
	for _, edge := range edges {
		key := sourceTypeKey(edge)
		targets, ok := index[key]
		if !ok {
			targets = make(map[string]struct{})
			index[key] = targets
		}
		targets[edge.TargetID] = struct{}{}
	}
	return index
}
