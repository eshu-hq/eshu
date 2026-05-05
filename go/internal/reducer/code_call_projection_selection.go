package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type codeCallSelectionResult struct {
	Key                         SharedProjectionAcceptanceKey
	BlockedReadiness            int
	MaxBlockedIntentWaitSeconds float64
	SelectionDurationSeconds    float64
}

func (r *CodeCallProjectionRunner) selectAcceptanceUnitWork(ctx context.Context) (SharedProjectionAcceptanceKey, error) {
	result, err := r.selectAcceptanceUnitWorkWithStats(ctx, time.Now().UTC())
	return result.Key, err
}

func (r *CodeCallProjectionRunner) selectAcceptanceUnitWorkWithStats(ctx context.Context, now time.Time) (codeCallSelectionResult, error) {
	start := time.Now()
	acceptanceTelemetry := sharedAcceptanceTelemetry{
		Instruments: r.Instruments,
		Logger:      r.Logger,
	}
	scanLimit := r.Config.batchLimit()
	acceptanceScanLimit := r.Config.acceptanceScanLimit()
	if scanLimit > acceptanceScanLimit {
		scanLimit = acceptanceScanLimit
	}

	for {
		pending, err := r.IntentReader.ListPendingDomainIntents(ctx, DomainCodeCalls, scanLimit)
		if err != nil {
			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "error",
				Duration: time.Since(start).Seconds(),
				Err:      err,
			})
			return codeCallSelectionResult{}, fmt.Errorf("list pending code call intents: %w", err)
		}
		if len(pending) == 0 {
			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "miss",
				Duration: time.Since(start).Seconds(),
			})
			return codeCallSelectionResult{SelectionDurationSeconds: time.Since(start).Seconds()}, nil
		}

		lookup := r.AcceptedGen
		if r.AcceptedGenPrefetch != nil {
			resolvedLookup, err := r.AcceptedGenPrefetch(ctx, pending)
			if err != nil {
				acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
					Runner:   "code_call_projection",
					Result:   "error",
					Duration: time.Since(start).Seconds(),
					Err:      err,
				})
				return codeCallSelectionResult{}, fmt.Errorf("prefetch accepted generations: %w", err)
			}
			lookup = resolvedLookup
		}

		phase, gated := sharedProjectionReadinessPhase(DomainCodeCalls)
		acceptedByKey := make(map[SharedProjectionAcceptanceKey]string, len(pending))
		seen := make(map[SharedProjectionAcceptanceKey]struct{}, len(pending))
		for _, row := range pending {
			key, ok := row.AcceptanceKey()
			if !ok {
				return codeCallSelectionResult{}, fmt.Errorf(
					"pending code call intent %q is missing scope, acceptance unit, or source run",
					row.IntentID,
				)
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if acceptedGeneration, ok := lookup(key); ok {
				acceptedByKey[key] = acceptedGeneration
			}
		}

		readinessLookup := r.ReadinessLookup
		if gated && r.ReadinessPrefetch != nil {
			readinessKeys := make([]GraphProjectionPhaseKey, 0, len(acceptedByKey))
			for key, acceptedGeneration := range acceptedByKey {
				readinessKey, ok := graphProjectionPhaseKeyForAcceptance(
					key,
					acceptedGeneration,
					GraphProjectionKeyspaceCodeEntitiesUID,
				)
				if !ok {
					continue
				}
				readinessKeys = append(readinessKeys, readinessKey)
			}
			resolvedLookup, err := r.ReadinessPrefetch(ctx, readinessKeys, phase)
			if err != nil {
				acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
					Runner:   "code_call_projection",
					Result:   "error",
					Duration: time.Since(start).Seconds(),
					Err:      err,
				})
				return codeCallSelectionResult{}, fmt.Errorf("prefetch graph projection readiness: %w", err)
			}
			readinessLookup = resolvedLookup
		}

		blockedCount := 0
		maxBlockedWait := 0.0
		seen = make(map[SharedProjectionAcceptanceKey]struct{}, len(pending))
		for _, row := range pending {
			key, ok := row.AcceptanceKey()
			if !ok {
				return codeCallSelectionResult{}, fmt.Errorf(
					"pending code call intent %q is missing scope, acceptance unit, or source run",
					row.IntentID,
				)
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			acceptedGeneration, ok := acceptedByKey[key]
			if !ok {
				continue
			}
			if gated && readinessLookup != nil {
				readinessKey, ok := graphProjectionPhaseKeyForAcceptance(
					key,
					acceptedGeneration,
					GraphProjectionKeyspaceCodeEntitiesUID,
				)
				if !ok {
					continue
				}
				ready, found := readinessLookup(readinessKey, phase)
				if !found || !ready {
					blockedCount++
					if wait := maxSharedIntentWaitSeconds(now, []SharedProjectionIntentRow{row}); wait > maxBlockedWait {
						maxBlockedWait = wait
					}
					continue
				}
			}

			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "hit",
				Duration: time.Since(start).Seconds(),
			})
			return codeCallSelectionResult{
				Key:                         key,
				BlockedReadiness:            blockedCount,
				MaxBlockedIntentWaitSeconds: maxBlockedWait,
				SelectionDurationSeconds:    time.Since(start).Seconds(),
			}, nil
		}

		if blockedCount > 0 && r.Logger != nil {
			r.Logger.InfoContext(
				ctx,
				"code call projection skipped acceptance units until canonical node readiness is committed",
				slog.Int("blocked_count", blockedCount),
				slog.Float64("blocked_intent_wait_seconds", maxBlockedWait),
				slog.String("domain", DomainCodeCalls),
				telemetry.PhaseAttr(telemetry.PhaseShared),
			)
		}

		if len(pending) < scanLimit {
			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "miss",
				Duration: time.Since(start).Seconds(),
			})
			return codeCallSelectionResult{
				BlockedReadiness:            blockedCount,
				MaxBlockedIntentWaitSeconds: maxBlockedWait,
				SelectionDurationSeconds:    time.Since(start).Seconds(),
			}, nil
		}
		if scanLimit >= acceptanceScanLimit {
			acceptanceTelemetry.RecordLookup(ctx, sharedAcceptanceLookupEvent{
				Runner:   "code_call_projection",
				Result:   "error",
				Duration: time.Since(start).Seconds(),
				Err: fmt.Errorf(
					"scan limit cap reached before finding accepted code call work (%d)",
					acceptanceScanLimit,
				),
			})
			return codeCallSelectionResult{}, fmt.Errorf(
				"code call acceptance scan reached cap (%d) before locating accepted work",
				acceptanceScanLimit,
			)
		}

		nextLimit := scanLimit * 2
		if nextLimit > acceptanceScanLimit {
			nextLimit = acceptanceScanLimit
		}
		scanLimit = nextLimit
	}
}
