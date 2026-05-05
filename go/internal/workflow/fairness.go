package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

const defaultFairnessWeight = 1

// FairnessCandidate is one claim-capable collector instance eligible for
// family-level scheduling.
type FairnessCandidate struct {
	CollectorKind       scope.CollectorKind
	CollectorInstanceID string
	Weight              int
}

// ClaimTarget identifies the collector instance that should attempt the next
// durable claim.
type ClaimTarget struct {
	CollectorKind       scope.CollectorKind
	CollectorInstanceID string
}

// FamilyFairnessScheduler performs deterministic weighted round-robin across
// collector families and deterministic rotation within each selected family.
type FamilyFairnessScheduler struct {
	families    []fairnessFamily
	totalWeight int
}

type fairnessFamily struct {
	collectorKind scope.CollectorKind
	weight        int
	currentWeight int
	instanceIDs   []string
	nextInstance  int
}

// NewFamilyFairnessScheduler builds a scheduler for the supplied eligible
// claim candidates.
func NewFamilyFairnessScheduler(candidates []FairnessCandidate) (*FamilyFairnessScheduler, error) {
	grouped := make(map[scope.CollectorKind]*fairnessFamily)
	for _, candidate := range candidates {
		kind := scope.CollectorKind(strings.TrimSpace(string(candidate.CollectorKind)))
		instanceID := strings.TrimSpace(candidate.CollectorInstanceID)
		if kind == "" {
			return nil, fmt.Errorf("collector kind is required")
		}
		if instanceID == "" {
			return nil, fmt.Errorf("collector instance id is required")
		}
		weight := candidate.Weight
		if weight == 0 {
			weight = defaultFairnessWeight
		}
		if weight < 0 {
			return nil, fmt.Errorf("fairness weight for collector family %q must be positive", kind)
		}

		family, ok := grouped[kind]
		if !ok {
			family = &fairnessFamily{collectorKind: kind, weight: weight}
			grouped[kind] = family
		}
		if weight > family.weight {
			family.weight = weight
		}
		family.instanceIDs = append(family.instanceIDs, instanceID)
	}

	families := make([]fairnessFamily, 0, len(grouped))
	for _, family := range grouped {
		sort.Strings(family.instanceIDs)
		families = append(families, *family)
	}
	sort.Slice(families, func(i, j int) bool {
		return families[i].collectorKind < families[j].collectorKind
	})

	totalWeight := 0
	for _, family := range families {
		totalWeight += family.weight
	}
	return &FamilyFairnessScheduler{families: families, totalWeight: totalWeight}, nil
}

// Next returns the collector instance that should get the next claim attempt.
func (s *FamilyFairnessScheduler) Next() (ClaimTarget, bool) {
	if s == nil || len(s.families) == 0 || s.totalWeight <= 0 {
		return ClaimTarget{}, false
	}

	selected := 0
	for i := range s.families {
		s.families[i].currentWeight += s.families[i].weight
		if s.families[i].currentWeight > s.families[selected].currentWeight {
			selected = i
		}
	}

	family := &s.families[selected]
	family.currentWeight -= s.totalWeight
	instanceID := family.instanceIDs[family.nextInstance]
	family.nextInstance = (family.nextInstance + 1) % len(family.instanceIDs)
	return ClaimTarget{
		CollectorKind:       family.collectorKind,
		CollectorInstanceID: instanceID,
	}, true
}

// FairnessCandidatesFromCollectorInstances extracts claim-enabled instances
// from durable collector state.
func FairnessCandidatesFromCollectorInstances(instances []CollectorInstance) ([]FairnessCandidate, error) {
	candidates := make([]FairnessCandidate, 0, len(instances))
	for _, instance := range instances {
		if !instance.Enabled || !instance.ClaimsEnabled {
			continue
		}
		weight, err := fairnessWeightFromConfiguration(instance.Configuration)
		if err != nil {
			return nil, fmt.Errorf("collector instance %q: %w", instance.InstanceID, err)
		}
		candidates = append(candidates, FairnessCandidate{
			CollectorKind:       instance.CollectorKind,
			CollectorInstanceID: instance.InstanceID,
			Weight:              weight,
		})
	}
	return candidates, nil
}

type fairnessConfiguration struct {
	FairnessWeight *int `json:"fairness_weight"`
}

// fairnessWeightFromConfiguration returns the explicit family weight, treating
// an omitted field as the default and an explicit non-positive value as invalid.
func fairnessWeightFromConfiguration(raw string) (int, error) {
	normalized := normalizeJSONDocument(raw)
	var cfg fairnessConfiguration
	if err := json.Unmarshal([]byte(normalized), &cfg); err != nil {
		return 0, fmt.Errorf("parse fairness configuration: %w", err)
	}
	if cfg.FairnessWeight == nil {
		return defaultFairnessWeight, nil
	}
	if *cfg.FairnessWeight <= 0 {
		return 0, fmt.Errorf("fairness_weight must be positive")
	}
	return *cfg.FairnessWeight, nil
}
