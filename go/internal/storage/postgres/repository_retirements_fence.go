// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Phase-1 mark-and-fence request budget and deadline (#7766). The 25
// repository cap is the request cap; the precheck bounds the cost: the
// non-superseded generation and 7d-predicate row sums refuse with 409
// request_too_large above these candidate ceilings. The ceilings are
// candidates the tuned mark-form measurement must confirm or lower (P1'):
// 20,000 reducer-row marks measured ~579 ms on the proof container at the
// ceiling, past the 250 ms fenced-section bar, so the runner increment
// re-measures on production-shape storage before raising them.
const (
	// RetirementFenceMaxRepositories caps one fence request.
	RetirementFenceMaxRepositories = 25
	// RetirementFenceMaxGenerations caps the summed non-superseded
	// generations one fence request may carry.
	RetirementFenceMaxGenerations = 5000
	// RetirementFenceMaxReducerRows caps the summed 7d-predicate reducer
	// rows one fence request may carry.
	RetirementFenceMaxReducerRows = 20000
)

const (
	// retirementFenceDeadline bounds the whole phase-1 request from
	// request start, leaving the response margin to the caller.
	retirementFenceDeadline = 5*time.Minute + 30*time.Second
	// retirementFenceReserve is the fence section plus steps 4 to 8
	// budget held back from the advisory wait.
	retirementFenceReserve = 35 * time.Second
	// retirementFenceDrainBound caps the scope-bound drain wait.
	retirementFenceDrainBound = 5 * time.Minute
	// retirementFenceBudget caps the fenced section (steps 3 to 8) across
	// restarts.
	retirementFenceBudget = 30 * time.Second
	// retirementFenceDrainPoll is the live-lease poll interval while the
	// transaction holds no locks.
	retirementFenceDrainPoll = 500 * time.Millisecond
	// retirementFenceRetryJitter bounds the 55P03 restart sleep.
	retirementFenceRetryJitter    = 100 * time.Millisecond
	retirementFenceRetryJitterMax = 500 * time.Millisecond

	// retirementFencedFailureClass marks every projector and reducer row
	// the fence supersedes, so operators and the residue census can tell
	// retired work from ordinary supersession.
	retirementFencedFailureClass = "repository_retired"
)

// Fence refusal reasons. Without a marker row these are API response
// reasons (the admin route maps them to 409); claim_fence_busy doubles as
// the 2q blocked_reason once a marker row exists.
const (
	// FenceReasonProjectorLeaseLive refuses while a projector Run holds
	// a live lease past the drain bound.
	FenceReasonProjectorLeaseLive = "projector_lease_live"
	// FenceReasonInflightReducers refuses while reducer work holds a live
	// lease past the drain bound.
	FenceReasonInflightReducers = "inflight_reducers"
	// FenceReasonLockTimeout refuses when the advisory wait or the fence
	// budget runs out.
	FenceReasonLockTimeout = "lock_timeout"
	// FenceReasonClaimFenceBusy refuses when the 1s LOCK TABLE wait keeps
	// losing past the fence budget.
	FenceReasonClaimFenceBusy = "claim_fence_busy"
	// FenceReasonRequestTooLarge refuses an over-budget request with
	// per-repository counts.
	FenceReasonRequestTooLarge = "request_too_large"
)

// FenceBlockedError is a bounded phase-1 refusal: the request stops before
// any marker exists, so the reason travels in the error, not the table.
type FenceBlockedError struct {
	// Reason is one of the FenceReason* values.
	Reason string
	// RepoIDs are the request's repositories, sorted.
	RepoIDs []string
	// Generations and ReducerRows carry the refusal sums on a
	// request_too_large refusal: the precheck snapshot, or the under-lock
	// recount over the locked scopes.
	Generations int64
	ReducerRows int64
	// PerRepo counts generations and 7d-predicate rows by repository on a
	// request_too_large refusal.
	PerRepo map[string]FenceRepoBudget
	// Detail names the wait or bound that could not finish.
	Detail string
}

// FenceRepoBudget carries one repository's precheck counts.
type FenceRepoBudget struct {
	Generations int64
	ReducerRows int64
}

func (e *FenceBlockedError) Error() string {
	if e == nil {
		return "retirement fence blocked"
	}
	return fmt.Sprintf("retirement fence blocked: %s (%s)", e.Reason, e.Detail)
}

// FenceRetirement is one repository's fence entry: the tombstone key, the
// canonical repo id (ingestion_scopes.partition_key), and the
// default-branch scope the marker covers. The fence resolves every @ref
// scope of the repository itself; the marker names the default-branch one.
type FenceRetirement struct {
	RetirementID string
	RepoID       string
	ScopeID      string
}

// FenceRequest is the operator's phase-1 fence input: 1 to 25
// repositories retired under one idempotency key, all or nothing.
type FenceRequest struct {
	Retirements []FenceRetirement
	// ReasonCode is always RetirementReasonOperatorRetired: retirement is
	// an explicit operator act, never an automatic policy outcome.
	ReasonCode string
	// ReasonHash is the safe hash of the operator's free-text reason.
	ReasonHash string
	// ActorClass names the authenticated actor class.
	ActorClass string
	// ActorIDHash is the safe hash of the actor identity.
	ActorIDHash string
	// IdempotencyKeyHash resumes the same rows on retry.
	IdempotencyKeyHash string
}

// FenceResult reports the committed phase-1 fence.
type FenceResult struct {
	// RetirementIDs are the committed marker ids in request order. A
	// resumed retry reports the existing row's id, not the request's.
	RetirementIDs []string
	// RepoIDs are the request's repositories, sorted.
	RepoIDs []string
	// ScopeIDs are the fenced scopes (default-branch plus ref scopes).
	ScopeIDs []string
	// GenerationsFenced counts superseded generations.
	GenerationsFenced int64
	// ProjectorRowsMarked counts superseded projector rows.
	ProjectorRowsMarked int64
	// ReducerRowsMarked counts superseded reducer rows.
	ReducerRowsMarked int64
}

// FenceOptions tunes the phase-1 fence. Zero values select the designed
// defaults; tests shrink the deadline and stub the sleep.
type FenceOptions struct {
	Deadline          time.Duration
	DrainPollInterval time.Duration
	RetryMinSleep     time.Duration
	RetryMaxSleep     time.Duration
	Sleep             func(time.Duration)
	Now               func() time.Time
	Logger            *slog.Logger
}

func (o FenceOptions) withDefaults() FenceOptions {
	if o.Deadline <= 0 {
		o.Deadline = retirementFenceDeadline
	}
	if o.DrainPollInterval <= 0 {
		o.DrainPollInterval = retirementFenceDrainPoll
	}
	if o.RetryMinSleep <= 0 {
		o.RetryMinSleep = retirementFenceRetryJitter
	}
	if o.RetryMaxSleep <= 0 {
		o.RetryMaxSleep = retirementFenceRetryJitterMax
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

// RetirementFencer runs the phase-1 mark-and-fence: the drain wait with no
// locks held, then steps 3 to 8 in one transaction (advisory key, scope
// rows, EXCLUSIVE table lock, tuned marks, marker, assertion, commit).
// Useful concurrency is preserved: only the retiring repositories'
// commits queue behind the per-repository exclusive key. The 1s LOCK
// TABLE wait is budget-bounded and restarts at step 1 on timeout, so no
// convoy forms — but while the EXCLUSIVE lock is held through the marks,
// marker write, and assertion, every fact_work_items writer cluster-wide
// stalls behind it.
type RetirementFencer struct {
	queryer  db.Queryer
	beginner db.Beginner
	// Options tunes deadlines and sleeps. Tests shrink them; production
	// uses the designed defaults.
	Options FenceOptions
}

// NewRetirementFencer binds the fence to pre-transaction reads and the
// fence transaction.
func NewRetirementFencer(queryer db.Queryer, beginner db.Beginner) RetirementFencer {
	return RetirementFencer{queryer: queryer, beginner: beginner}
}

// FenceRepository runs the phase-1 mark-and-fence for the request's
// repositories, all or nothing, under one deadline. A live lease or a
// lock-timeout rolls back and restarts at step 1 with every wait
// recomputed from the remaining deadline; only an exhausted bound, an
// over-budget precheck, or an assertion mismatch stops the request.
func (f RetirementFencer) FenceRepository(ctx context.Context, req FenceRequest) (FenceResult, error) {
	opts := f.Options.withDefaults()
	repoIDs, err := validatedFenceRepos(req)
	if err != nil {
		return FenceResult{}, err
	}
	if f.queryer == nil || f.beginner == nil {
		return FenceResult{}, fmt.Errorf("retirement fence: queryer and beginner are required")
	}
	deadline := opts.Now().Add(opts.Deadline)
	lastReason := FenceReasonClaimFenceBusy
	var fenceSpent time.Duration
	var sectionBudget time.Duration
	var sectionBudgetSet bool
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return FenceResult{}, &FenceBlockedError{Reason: lastReason, RepoIDs: repoIDs, Detail: "phase-1 deadline exhausted"}
		}
		if err := ctx.Err(); err != nil {
			return FenceResult{}, err
		}
		scopeIDs, blocked, err := f.precheck(ctx, repoIDs)
		if err != nil {
			return FenceResult{}, err
		}
		if blocked != nil {
			return FenceResult{}, blocked
		}
		reason, err := f.drainLeases(ctx, opts, deadline, scopeIDs)
		if err != nil {
			return FenceResult{}, err
		}
		if reason != "" {
			return FenceResult{}, &FenceBlockedError{Reason: reason, RepoIDs: repoIDs, Detail: "drain bound exhausted with a live lease"}
		}
		if !sectionBudgetSet {
			sectionBudgetSet = true
			sectionBudget = retirementFenceBudget
			if remaining := time.Until(deadline); remaining < sectionBudget {
				sectionBudget = remaining
			}
		}
		attemptStart := opts.Now()
		result, restart, err := f.fenceAttempt(ctx, opts, deadline, req, repoIDs)
		fenceSpent += opts.Now().Sub(attemptStart)
		if err != nil {
			return FenceResult{}, err
		}
		if !restart {
			return result, nil
		}
		if fenceSpent >= sectionBudget {
			return FenceResult{}, &FenceBlockedError{
				Reason: FenceReasonClaimFenceBusy, RepoIDs: repoIDs,
				Detail: "fence section budget exhausted across restarts",
			}
		}
		lastReason = FenceReasonClaimFenceBusy
		opts.Sleep(retryJitter(opts))
	}
}

// validatedFenceRepos checks the request shape: 1 to 25 repositories,
// every id and the operator reason present, no duplicate repository.
func validatedFenceRepos(req FenceRequest) ([]string, error) {
	if len(req.Retirements) == 0 || len(req.Retirements) > RetirementFenceMaxRepositories {
		return nil, fmt.Errorf("retirement fence: want 1 to %d repositories, got %d",
			RetirementFenceMaxRepositories, len(req.Retirements))
	}
	if req.ReasonCode != RetirementReasonOperatorRetired {
		return nil, fmt.Errorf("retirement fence: reason %q is not operator-driven", req.ReasonCode)
	}
	if strings.TrimSpace(req.IdempotencyKeyHash) == "" {
		return nil, fmt.Errorf("retirement fence: empty idempotency key")
	}
	repoIDs := make([]string, 0, len(req.Retirements))
	seen := make(map[string]struct{}, len(req.Retirements))
	for _, retirement := range req.Retirements {
		if strings.TrimSpace(retirement.RetirementID) == "" {
			return nil, fmt.Errorf("retirement fence: empty retirement id")
		}
		if strings.TrimSpace(retirement.RepoID) == "" {
			return nil, fmt.Errorf("retirement fence: empty repo id")
		}
		if strings.TrimSpace(retirement.ScopeID) == "" {
			return nil, fmt.Errorf("retirement fence: empty scope id")
		}
		if _, dup := seen[retirement.RepoID]; dup {
			return nil, fmt.Errorf("retirement fence: duplicate repo id %q", retirement.RepoID)
		}
		seen[retirement.RepoID] = struct{}{}
		repoIDs = append(repoIDs, retirement.RepoID)
	}
	sort.Strings(repoIDs)
	return repoIDs, nil
}

// precheck resolves the request's scopes and counts the non-superseded
// generations plus the 7d-predicate reducer rows per repository. It holds
// no locks. An over-budget sum refuses with request_too_large and the
// per-repository counts before any lock is taken. Step 4 re-verifies both
// sums under the advisory key; this snapshot only bounds the wait.
func (f RetirementFencer) precheck(
	ctx context.Context,
	repoIDs []string,
) ([]string, *FenceBlockedError, error) {
	rows, err := f.queryer.QueryContext(ctx, fenceResolveScopesQuery, repoIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("retirement fence resolve scopes: %w", err)
	}
	scopeIDs, resolveErr := scanResolveScopes(rows)
	if resolveErr != nil {
		return nil, nil, resolveErr
	}
	budgets := make(map[string]FenceRepoBudget, len(repoIDs))
	for _, repoID := range repoIDs {
		budgets[repoID] = FenceRepoBudget{}
	}
	var generations, reducerRows int64
	for _, query := range []struct {
		text    string
		reducer bool
	}{{
		text: fencePrecheckGenerationsQuery,
	}, {
		text:    fencePrecheckReducerRowsQuery,
		reducer: true,
	}} {
		counts, err := scanRepoCounts(ctx, f.queryer, query.text, repoIDs)
		if err != nil {
			return nil, nil, err
		}
		for repoID, count := range counts {
			budget := budgets[repoID]
			if query.reducer {
				budget.ReducerRows = count
				reducerRows += count
			} else {
				budget.Generations = count
				generations += count
			}
			budgets[repoID] = budget
		}
	}
	if generations > RetirementFenceMaxGenerations || reducerRows > RetirementFenceMaxReducerRows {
		return nil, &FenceBlockedError{
			Reason:      FenceReasonRequestTooLarge,
			RepoIDs:     repoIDs,
			Generations: generations,
			ReducerRows: reducerRows,
			PerRepo:     budgets,
			Detail: fmt.Sprintf("precheck sums %d generations and %d reducer rows exceed %d/%d",
				generations, reducerRows, RetirementFenceMaxGenerations, RetirementFenceMaxReducerRows),
		}, nil
	}
	return scopeIDs, nil, nil
}

// drainLeases waits with no locks held until no scope row holds a live
// lease in either stage. It returns the refusal reason when the bound
// (min of the drain bound and the remaining deadline) runs out, or empty
// when the drain cleared.
func (f RetirementFencer) drainLeases(
	ctx context.Context,
	opts FenceOptions,
	deadline time.Time,
	scopeIDs []string,
) (string, error) {
	bound := retirementFenceDrainBound
	if remaining := time.Until(deadline); remaining < bound {
		bound = remaining
	}
	stopAt := opts.Now().Add(bound)
	for {
		for _, wait := range []struct {
			query  string
			reason string
		}{
			{query: fenceLiveProjectorLeasesQuery, reason: FenceReasonProjectorLeaseLive},
			{query: fenceLiveReducerLeasesQuery, reason: FenceReasonInflightReducers},
		} {
			live, err := hasRows(ctx, f.queryer, wait.query, scopeIDs)
			if err != nil {
				return "", err
			}
			if live {
				if !opts.Now().Before(stopAt) {
					return wait.reason, nil
				}
				if err := sleepContext(ctx, opts.DrainPollInterval); err != nil {
					return "", err
				}
				break
			}
			if wait.reason == FenceReasonInflightReducers {
				return "", nil
			}
		}
	}
}
