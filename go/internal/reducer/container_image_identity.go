package reducer

import (
	"context"
	"fmt"
	"sort"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ContainerImageIdentityOutcome names the reducer decision for one image
// reference seen in Git or runtime evidence.
type ContainerImageIdentityOutcome string

const (
	// ContainerImageIdentityExactDigest means the source reference already
	// named a digest also observed in registry facts.
	ContainerImageIdentityExactDigest ContainerImageIdentityOutcome = "exact_digest"
	// ContainerImageIdentityTagResolved means one registry tag observation
	// resolved the source tag to exactly one digest.
	ContainerImageIdentityTagResolved ContainerImageIdentityOutcome = "tag_resolved"
	// ContainerImageIdentityAmbiguousTag means tag observations for the same
	// image reference point at multiple digests.
	ContainerImageIdentityAmbiguousTag ContainerImageIdentityOutcome = "ambiguous_tag"
	// ContainerImageIdentityUnresolved means no registry digest observation
	// matched the source image reference.
	ContainerImageIdentityUnresolved ContainerImageIdentityOutcome = "unresolved"
	// ContainerImageIdentityStaleTag means runtime evidence resolved a tag to
	// a digest that registry facts report as the previous digest.
	ContainerImageIdentityStaleTag ContainerImageIdentityOutcome = "stale_tag"
)

// ContainerImageIdentityDecision records one bounded image identity decision.
type ContainerImageIdentityDecision struct {
	ImageRef         string
	Digest           string
	RepositoryID     string
	Outcome          ContainerImageIdentityOutcome
	Reason           string
	CanonicalWrites  int
	EvidenceFactIDs  []string
	IdentityStrength string
}

// ContainerImageIdentityWrite carries decisions for durable publication.
type ContainerImageIdentityWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []ContainerImageIdentityDecision
}

// ContainerImageIdentityWriteResult summarizes durable publication.
type ContainerImageIdentityWriteResult struct {
	CanonicalWrites int
	EvidenceSummary string
}

// ContainerImageIdentityWriter persists reducer-owned image identity truth.
type ContainerImageIdentityWriter interface {
	WriteContainerImageIdentityDecisions(
		context.Context,
		ContainerImageIdentityWrite,
	) (ContainerImageIdentityWriteResult, error)
}

type activeContainerImageIdentityFactLoader interface {
	ListActiveContainerImageIdentityFacts(ctx context.Context) ([]facts.Envelope, error)
}

// ContainerImageIdentityHandler joins Git/runtime image references with active
// OCI registry facts and publishes digest-keyed identity decisions.
type ContainerImageIdentityHandler struct {
	FactLoader  FactLoader
	Writer      ContainerImageIdentityWriter
	Instruments *telemetry.Instruments
}

// Handle executes one container image identity reducer intent.
func (h ContainerImageIdentityHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainContainerImageIdentity {
		return Result{}, fmt.Errorf("container_image_identity handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("container image identity fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("container image identity writer is required")
	}

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		containerImageIdentityFactKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load container image identity facts: %w", err)
	}
	active, err := h.loadActiveContainerImageIdentityFacts(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load active container image identity facts: %w", err)
	}
	envelopes = append(envelopes, active...)

	decisions := BuildContainerImageIdentityDecisions(envelopes)
	counts := containerImageIdentityCounts(decisions)

	writeResult, err := h.Writer.WriteContainerImageIdentityDecisions(ctx, ContainerImageIdentityWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write container image identity decisions: %w", err)
	}

	h.emitCounters(ctx, counts)

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainContainerImageIdentity,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: containerImageIdentitySummary(
			len(decisions),
			counts,
			writeResult.CanonicalWrites,
		),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func (h ContainerImageIdentityHandler) loadActiveContainerImageIdentityFacts(
	ctx context.Context,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeContainerImageIdentityFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveContainerImageIdentityFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

func (h ContainerImageIdentityHandler) emitCounters(
	ctx context.Context,
	counts map[ContainerImageIdentityOutcome]int,
) {
	if h.Instruments == nil {
		return
	}
	for _, outcome := range containerImageIdentityOutcomes() {
		count := counts[outcome]
		if count == 0 {
			continue
		}
		h.Instruments.ContainerImageIdentityDecisions.Add(
			ctx,
			int64(count),
			metric.WithAttributes(
				telemetry.AttrDomain(string(DomainContainerImageIdentity)),
				telemetry.AttrOutcome(string(outcome)),
			),
		)
	}
}

// BuildContainerImageIdentityDecisions classifies source image references
// against OCI registry observations.
func BuildContainerImageIdentityDecisions(envelopes []facts.Envelope) []ContainerImageIdentityDecision {
	refs := extractContainerImageRefs(envelopes)
	index := buildContainerImageRegistryIndex(envelopes)
	decisions := make([]ContainerImageIdentityDecision, 0, len(refs))
	for _, ref := range refs {
		decisions = append(decisions, classifyContainerImageRef(ref, index))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		return decisions[i].ImageRef < decisions[j].ImageRef
	})
	return decisions
}

func containerImageIdentityFactKinds() []string {
	return []string{
		factKindContentEntity,
		facts.AWSRelationshipFactKind,
		facts.AWSImageReferenceFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageReferrerFactKind,
	}
}

func containerImageIdentityOutcomes() []ContainerImageIdentityOutcome {
	return []ContainerImageIdentityOutcome{
		ContainerImageIdentityExactDigest,
		ContainerImageIdentityTagResolved,
		ContainerImageIdentityAmbiguousTag,
		ContainerImageIdentityUnresolved,
		ContainerImageIdentityStaleTag,
	}
}

func containerImageIdentityCounts(
	decisions []ContainerImageIdentityDecision,
) map[ContainerImageIdentityOutcome]int {
	counts := make(map[ContainerImageIdentityOutcome]int, len(containerImageIdentityOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

func containerImageIdentitySummary(
	evaluated int,
	counts map[ContainerImageIdentityOutcome]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"container image identity evaluated=%d exact_digest=%d tag_resolved=%d ambiguous_tag=%d unresolved=%d stale_tag=%d canonical_writes=%d",
		evaluated,
		counts[ContainerImageIdentityExactDigest],
		counts[ContainerImageIdentityTagResolved],
		counts[ContainerImageIdentityAmbiguousTag],
		counts[ContainerImageIdentityUnresolved],
		counts[ContainerImageIdentityStaleTag],
		canonicalWrites,
	)
}

func containerImageIdentityCanonicalDecisions(
	decisions []ContainerImageIdentityDecision,
) []ContainerImageIdentityDecision {
	out := make([]ContainerImageIdentityDecision, 0, len(decisions))
	for _, decision := range decisions {
		if decision.CanonicalWrites <= 0 {
			continue
		}
		out = append(out, decision)
	}
	return out
}
