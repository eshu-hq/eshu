// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// The manifest-dependency trigger (#9874, arbiter ruling Option B). The
// projector enqueues one supply_chain_impact intent per git scope generation
// whose only trigger is a manifest or lockfile dependency row, and stamps the
// payload with this constant marker. The handler branches on it before it
// loads any evidence: it writes no findings, issues no fencing token, and only
// reopens the scopes that hold advisory evidence for the packages the
// generation changed. Those reopened passes re-derive their findings from the
// current manifest rows, which is what retracts or adds the finding.
const (
	// PayloadKeyTrigger is the reducer work-item payload key the projector sets
	// on a marker intent.
	PayloadKeyTrigger = "trigger"
	// TriggerManifestDependency is the PayloadKeyTrigger value for a git
	// generation whose only trigger fact is a dependency row.
	TriggerManifestDependency = "manifest_dependency"
)

// ManifestGenerationActivation is the marker generation's place relative to
// its scope's active generation.
type ManifestGenerationActivation string

// The closed activation set the ruling's gate needs.
const (
	// ManifestGenerationActive means the generation is the scope's active
	// generation: fan out.
	ManifestGenerationActive ManifestGenerationActivation = "active"
	// ManifestGenerationPending means the generation has not activated yet
	// (no active generation, or an older one is still active): defer. Under
	// the default runtime this is unreachable: Runtime.GenerationCheck defers
	// a pending intent before Handle with the shared non-counting
	// generation_activation_not_ready class, so the branch keeps the arm only
	// for a runtime without the guard.
	ManifestGenerationPending ManifestGenerationActivation = "pending"
	// ManifestGenerationSuperseded means a newer generation, failure, or
	// retention replaced it: succeed without work; the newer generation's own
	// pass owns the change.
	ManifestGenerationSuperseded ManifestGenerationActivation = "superseded"
)

// ManifestDependencyView is one generation's dependency evidence.
type ManifestDependencyView struct {
	// GenerationID names the generation the view was read from.
	GenerationID string
	// Rows are the generation's own git dependency rows, tombstones included,
	// plus its repository fact when the generation is a delta (the fact that
	// names the delta's changed and deleted paths).
	Rows []facts.Envelope
	// Base is the dependency rows of a delta generation's base full
	// generation, the rows facts.OverlayDeltaBase overlays Rows on. Only the
	// prior view needs it.
	Base []facts.Envelope
	// BaseMissing reports a delta whose base could not be resolved (missing,
	// pruned, or unconfirmed). The view is then the delta's own rows only.
	BaseMissing bool
}

// ManifestDependencyGeneration is what a marker pass reads about its own
// generation and the activated chain behind it (arbiter ruling D2/D3).
type ManifestDependencyGeneration struct {
	Activation ManifestGenerationActivation
	Current    ManifestDependencyView
	// Baseline is the watermark W: the newest activated generation before
	// Current whose own marker pass completed (its work item carries
	// PayloadKeyManifestTriggerCompleted). HasBaseline is false when no prior
	// generation was ever stamped.
	Baseline    ManifestDependencyView
	HasBaseline bool
	// Chain is the activated generations strictly between Baseline (or, with
	// no baseline, the scope's first generation) and Current, oldest first.
	// A generation whose facts retention pruned is left out and ChainPruned is
	// set.
	Chain       []ManifestDependencyView
	ChainPruned bool
	// Degraded reports that the chain could not be diffed: it was longer than
	// ManifestTriggerChainDepthCap, or the baseline's facts were pruned. Every
	// current pin then counts as changed.
	Degraded bool
}

// ManifestTriggerChainDepthCap bounds how many activated generations a
// marker pass diffs between its baseline and itself.
const ManifestTriggerChainDepthCap = 32

// PayloadKeyManifestTriggerCompleted is the work-item payload key a marker
// pass stamps on its own item when it reopened every holder it owed (or found
// nothing to reopen). The next marker pass of the scope diffs against the
// newest generation carrying it.
const PayloadKeyManifestTriggerCompleted = "manifest_trigger_completed"

// ManifestDependencyGenerationReader reads a marker pass's generation. The
// Postgres SupplyChainImpactFanOutStore implements it beside the fan-out port;
// the handler asserts it on SupplyChainImpactHandler.FanOut so the branch adds
// no handler field.
type ManifestDependencyGenerationReader interface {
	ReadManifestDependencyGeneration(ctx context.Context, scopeID, generationID string) (ManifestDependencyGeneration, error)
	// RecordManifestTriggerCompleted stamps the calling pass's own item with
	// PayloadKeyManifestTriggerCompleted, fenced on its lease like an
	// acknowledgement. It fails when the lease is no longer held.
	RecordManifestTriggerCompleted(ctx context.Context, intent reducercontract.Intent, baselineGenerationID string) error
}

// PackageHolderCandidate is one active vulnerability.affected_package identity
// the package-keyed holder read returns. The handler matches it against the
// changed dependency keys with affectedPackageConsumptionKeys, the same join
// the findings pass uses, so the read itself can stay a coarse prefilter.
type PackageHolderCandidate struct {
	ScopeID     string
	Ecosystem   string
	PackageName string
	PURL        string
	PackageID   string
}

// handleManifestTrigger is Handle's one early return: it reports handled=false
// for every intent without the marker, which then runs the findings pass.
func (h SupplyChainImpactHandler) handleManifestTrigger(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, bool, error) {
	if trigger, _ := intent.Payload[PayloadKeyTrigger].(string); trigger != TriggerManifestDependency {
		return reducercontract.Result{}, false, nil
	}
	result, err := h.runManifestTrigger(ctx, intent)
	return result, true, err
}

// runManifestTrigger gates on activation, derives the changed packages, and
// reopens their holders. A continuation claim (holders left over from the cap)
// drains the stored list without re-reading the generation.
func (h SupplyChainImpactHandler) runManifestTrigger(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	signals := map[string]float64{"manifest_trigger": 1}
	if h.FanOut == nil {
		return manifestTriggerResult(intent, "manifest_dependency trigger: no fan-out port wired", signals), nil
	}
	reader, ok := h.FanOut.(ManifestDependencyGenerationReader)
	if !ok {
		return reducercontract.Result{}, fmt.Errorf("supply chain impact manifest trigger needs a fan-out port that reads manifest generations")
	}
	state := FanOutState{ConsumeOrigin: fanOutOriginStamped(intent)}
	if state.ConsumeOrigin {
		state.ExpectedOrigin = fanOutOriginOf(intent)
	}
	remaining := fanOutRemaining(intent)
	holders := remaining
	// baseline is the stamp's baseline_generation_id; a continuation claim
	// does not re-read the generation, so its stamp names no baseline.
	baseline := ""
	if len(remaining) == 0 {
		generation, err := reader.ReadManifestDependencyGeneration(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			h.countFanOut(ctx, fanOutCauseManifestDependency, fanOutOutcomeReadError, 1)
			return reducercontract.Result{}, fanOutError{fmt.Errorf("read manifest dependency generation: %w", err)}
		}
		switch generation.Activation {
		case ManifestGenerationActive:
		case ManifestGenerationPending:
			// Defensive: unreachable under the default runtime, whose
			// GenerationCheck defers this intent before Handle. Defer the
			// same way it would, with the shared non-counting class, which
			// ends when the generation activates or turns terminal (the
			// runtime then skips it as superseded).
			return reducercontract.Result{}, reducercontract.GenerationNotYetActiveError{
				ScopeID: intent.ScopeID, GenerationID: intent.GenerationID,
			}
		default:
			// Reachable only in a race: a newer generation activated after
			// the runtime's GenerationCheck passed this intent.
			signals["manifest_trigger_superseded"] = 1
			return h.finishManifestTrigger(ctx, intent, reader, state, nil, "manifest_dependency trigger: generation superseded", signals)
		}
		if generation.HasBaseline {
			baseline = generation.Baseline.GenerationID
		}
		change := changedManifestDependencyKeys(generation)
		change.mergeSubSignals(signals)
		if len(change.keys) == 0 {
			return h.finishManifestTrigger(ctx, intent, reader, state, &baseline, "manifest_dependency trigger: no changed package", signals)
		}
		candidates, err := h.FanOut.FindPackageHolderCandidates(ctx, intent.ScopeID, change.ecosystems())
		if err != nil {
			h.countFanOut(ctx, fanOutCauseManifestDependency, readFailureOutcome(err), 1)
			return reducercontract.Result{}, fanOutError{fmt.Errorf("find supply chain impact package holders: %w", err)}
		}
		holders = matchPackageHolders(intent.ScopeID, candidates, change.keys)
	}
	return h.reopenManifestHolders(ctx, intent, reader, state, holders, len(remaining) > 0, baseline, signals)
}

// reopenManifestHolders reopens one capped batch of holders, carries the rest
// on the item's continuation list, and records the pass. Only the claim that
// leaves no holder behind writes the completion stamp (ruling D1): a first
// chunk with a continuation list has not reopened everything it owes.
func (h SupplyChainImpactHandler) reopenManifestHolders(
	ctx context.Context,
	intent reducercontract.Intent,
	reader ManifestDependencyGenerationReader,
	state FanOutState,
	holders []string,
	continuation bool,
	baseline string,
	signals map[string]float64,
) (reducercontract.Result, error) {
	ctx, span := h.startFanOutSpan(ctx, intent)
	defer span.End()
	batch, rest := nextFanOutChunk(holders, fanOutHolderCap)
	result := fanOutResult{cause: fanOutCauseManifestDependency, holders: len(holders), deferred: len(rest)}
	if !continuation {
		result.newlyDeferred = len(rest)
	}
	origin := FanOutOrigin{SourceScopeID: intent.ScopeID, SourceGenerationID: intent.GenerationID, SourceIntentID: intent.IntentID}
	for _, holder := range batch {
		outcome, err := h.FanOut.ReopenHolder(ctx, holder, origin)
		if err != nil {
			h.countFanOut(ctx, result.cause, fanOutOutcomeReopenError, 1)
			span.RecordError(err)
			return reducercontract.Result{}, fanOutError{fmt.Errorf("reopen supply chain impact holder %q: %w", holder, err)}
		}
		result.record(outcome)
		h.logFanOutHolder(ctx, intent, holder, outcome)
	}
	if len(rest) > 0 || continuation {
		state.ReplaceRemaining = true
		state.Remaining = rest
	}
	if state.ReplaceRemaining || state.ConsumeOrigin {
		if err := h.FanOut.UpdateFanOutState(ctx, intent, state); err != nil {
			h.countFanOut(ctx, result.cause, fanOutOutcomeStateError, 1)
			span.RecordError(err)
			return reducercontract.Result{}, fanOutError{fmt.Errorf("record supply chain impact fan-out state: %w", err)}
		}
	}
	if len(rest) == 0 {
		if err := h.stampManifestTrigger(ctx, intent, reader, baseline, signals); err != nil {
			span.RecordError(err)
			return reducercontract.Result{}, err
		}
	}
	h.emitFanOut(ctx, intent, result)
	span.SetAttributes(
		attribute.String("cause", result.cause),
		attribute.Int("holders", result.holders),
		attribute.Int("reopened", result.reopened),
		attribute.Int("replay_flagged", result.replayFlagged),
		attribute.Int("deferred", result.deferred),
	)
	result.mergeSubSignals(signals)
	summary := fmt.Sprintf("manifest_dependency trigger: holders=%d reopened=%d deferred=%d", result.holders, result.reopened, result.deferred)
	return manifestTriggerResult(intent, summary, signals), nil
}

// finishManifestTrigger ends a pass that reopens nothing, consuming a fan-out
// stamp it carries so a later reopen by another path is judged fresh. A
// no-change pass is complete and writes the completion stamp (stampBaseline
// set); a superseded pass is not and writes none.
func (h SupplyChainImpactHandler) finishManifestTrigger(
	ctx context.Context,
	intent reducercontract.Intent,
	reader ManifestDependencyGenerationReader,
	state FanOutState,
	stampBaseline *string,
	summary string,
	signals map[string]float64,
) (reducercontract.Result, error) {
	if state.ConsumeOrigin {
		if err := h.FanOut.UpdateFanOutState(ctx, intent, state); err != nil {
			h.countFanOut(ctx, fanOutCauseManifestDependency, fanOutOutcomeStateError, 1)
			return reducercontract.Result{}, fanOutError{fmt.Errorf("clear supply chain impact fan-out state: %w", err)}
		}
	}
	if stampBaseline != nil {
		if err := h.stampManifestTrigger(ctx, intent, reader, *stampBaseline, signals); err != nil {
			return reducercontract.Result{}, err
		}
	}
	return manifestTriggerResult(intent, summary, signals), nil
}

// stampManifestTrigger writes the completion stamp. A failed stamp fails the
// pass retryably: the retry re-derives, finds its holders already queued, and
// stamps then, so a completed fan-out is never recorded without its stamp
// being durable.
func (h SupplyChainImpactHandler) stampManifestTrigger(
	ctx context.Context,
	intent reducercontract.Intent,
	reader ManifestDependencyGenerationReader,
	baseline string,
	signals map[string]float64,
) error {
	if err := reader.RecordManifestTriggerCompleted(ctx, intent, baseline); err != nil {
		h.countFanOut(ctx, fanOutCauseManifestDependency, fanOutOutcomeStateError, 1)
		return fanOutError{fmt.Errorf("record supply chain impact manifest trigger completion: %w", err)}
	}
	signals["manifest_trigger_completed"] = 1
	return nil
}

func manifestTriggerResult(intent reducercontract.Intent, summary string, signals map[string]float64) reducercontract.Result {
	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainSupplyChainImpact,
		Status:          reducercontract.ResultStatusSucceeded,
		EvidenceSummary: summary,
		SubSignals:      signals,
	}
}

// matchPackageHolders returns the sorted scopes, other than ownScopeID, whose
// affected-package identity shares a consumption key with a changed
// dependency. affectedPackageConsumptionKeys is the findings pass's own join.
func matchPackageHolders(ownScopeID string, candidates []PackageHolderCandidate, keys map[string]struct{}) []string {
	holders := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate.ScopeID == "" || candidate.ScopeID == ownScopeID {
			continue
		}
		if _, seen := holders[candidate.ScopeID]; seen {
			continue
		}
		pkg := supplychainmodel.AffectedPackage{
			Ecosystem: candidate.Ecosystem,
			Name:      candidate.PackageName,
			PURL:      candidate.PURL,
			PackageID: candidate.PackageID,
		}
		for _, key := range affectedPackageConsumptionKeys(pkg) {
			if _, changed := keys[key]; changed {
				holders[candidate.ScopeID] = struct{}{}
				break
			}
		}
	}
	ordered := make([]string, 0, len(holders))
	for scope := range holders {
		ordered = append(ordered, scope)
	}
	sort.Strings(ordered)
	return ordered
}

// keyEcosystem returns the ecosystem half of an "ecosystem\x00name" key.
func keyEcosystem(key string) string {
	ecosystem, _, _ := strings.Cut(key, "\x00")
	return ecosystem
}
