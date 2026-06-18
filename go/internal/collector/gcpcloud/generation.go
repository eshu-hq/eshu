package gcpcloud

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const tagSourceKindLabel = "label"

// ErrStaleGeneration reports that a generation was rejected because a newer
// generation (higher fencing token) already owns the scope. Callers classify it
// as terminal so a stale scan does not replace current facts or readiness.
var ErrStaleGeneration = errors.New("gcp generation rejected by newer fencing token")

// Generation accumulates resource observations and collection warnings for one
// bounded GCP collector scan, then builds a deterministic, deduplicated set of
// source-fact envelopes. It is the fixture-driven seam: callers parse CAI pages
// and feed AddPage/AddWarning, then Build commits a stable generation.
//
// Generation is not safe for concurrent use; one claim worker owns one
// Generation. Cross-scope ownership and fencing live in GenerationTracker.
type Generation struct {
	boundary  Boundary
	key       redact.Key
	resources map[string]ResourceObservation
	apiTags   []TagObservation
	warnings  []WarningObservation
	pageCount int
}

// NewGeneration creates a generation accumulator bound to one claim boundary and
// redaction key.
func NewGeneration(boundary Boundary, key redact.Key) *Generation {
	return &Generation{
		boundary:  boundary,
		key:       key,
		resources: make(map[string]ResourceObservation),
	}
}

// AddPage adds one parsed page of resource observations. Duplicate deliveries of
// the same resource within a generation converge: the resource is keyed by its
// full resource name plus asset type, so re-adding the same page does not
// produce duplicate facts. AddPage increments the page count for telemetry.
func (g *Generation) AddPage(resources []ResourceObservation) error {
	g.pageCount++
	for _, obs := range resources {
		key := resourceDedupeKey(obs)
		if key == "" {
			return errors.New("gcp resource observation missing full_resource_name")
		}
		g.resources[key] = obs
	}
	return nil
}

// AddWarning records one explicit collection warning for the generation.
func (g *Generation) AddWarning(obs WarningObservation) {
	g.warnings = append(g.warnings, obs)
}

// AddTagObservation records one direct or effective tag observation fetched
// outside the CAI resource page. The observation is emitted as source evidence
// only; downstream reducers attach it only to already-admitted resources.
func (g *Generation) AddTagObservation(obs TagObservation) {
	obs.Boundary = g.boundary
	g.apiTags = append(g.apiTags, obs)
}

// ObserveReadTime records a provider read time for the generation. When multiple
// pages carry read times, the latest non-zero read time is kept for generation
// payloads and freshness-lag telemetry.
func (g *Generation) ObserveReadTime(readTime time.Time) {
	readTime = readTime.UTC()
	if readTime.IsZero() {
		return
	}
	if g.boundary.ReadTime.IsZero() || readTime.After(g.boundary.ReadTime) {
		g.boundary.ReadTime = readTime
	}
}

// Build produces the deterministic, sorted envelope set for the generation.
// Resource facts are sorted by stable fact key so re-emitting the same
// generation yields the same order; warning facts follow in insertion order.
func (g *Generation) Build() ([]facts.Envelope, error) {
	keys := make([]string, 0, len(g.resources))
	for key := range g.resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	envelopes := make([]facts.Envelope, 0, g.envelopeCapacity())
	for _, key := range keys {
		resource := g.resources[key]
		env, err := NewCloudResourceEnvelope(g.boundary, resource, g.key)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
		tagEnv, ok, err := g.tagObservationEnvelope(resource)
		if err != nil {
			return nil, err
		}
		if ok {
			envelopes = append(envelopes, tagEnv)
		}
		relationshipEnvelopes, err := g.relationshipEnvelopes(resource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationshipEnvelopes...)
		iamEnvelopes, err := g.iamPolicyObservationEnvelopes(resource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, iamEnvelopes...)
		dnsEnvelopes, err := g.dnsRecordEnvelopes(resource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, dnsEnvelopes...)
		imageEnvelopes, err := g.imageReferenceEnvelopes(resource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, imageEnvelopes...)
	}
	secretsIAMEnvelopes, err := g.secretsIAMEnvelopes()
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, secretsIAMEnvelopes...)
	apiTagEnvelopes, err := g.apiTagEnvelopes()
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, apiTagEnvelopes...)
	for _, warning := range g.warnings {
		env, err := NewCollectionWarningEnvelope(warning)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	return envelopes, nil
}

// Boundary returns the claim boundary the generation was created with. The
// runtime uses it to build warning observations (for example page-token
// expiry) that share the generation's scope and fencing identity.
func (g *Generation) Boundary() Boundary { return g.boundary }

// PageCount returns the number of pages added to the generation.
func (g *Generation) PageCount() int { return g.pageCount }

// ResourceCount returns the number of deduplicated resources in the generation.
func (g *Generation) ResourceCount() int { return len(g.resources) }

// WarningCount returns the number of collection warnings in the generation.
func (g *Generation) WarningCount() int { return len(g.warnings) }

func (g *Generation) envelopeCapacity() int {
	capacity := len(g.resources) + len(g.warnings) + len(g.apiTags)
	for _, resource := range g.resources {
		capacity += len(resource.Relationships)
	}
	if g.key.IsZero() {
		return capacity
	}
	for _, resource := range g.resources {
		if hasUsableTags(resource.Labels) {
			capacity++
		}
		capacity += iamPolicyObservationCount(resource.IAMPolicyBindings)
		capacity += dnsRecordObservationCount(resource.DNSRecords)
		capacity += imageReferenceObservationCount(resource.ImageReferences)
	}
	return capacity
}

func (g *Generation) apiTagEnvelopes() ([]facts.Envelope, error) {
	if g.key.IsZero() || len(g.apiTags) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(g.apiTags))
	for _, obs := range g.apiTags {
		env, err := NewTagObservationEnvelope(obs, g.key)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	sort.Slice(envelopes, func(i, j int) bool {
		return envelopes[i].StableFactKey < envelopes[j].StableFactKey
	})
	return envelopes, nil
}

func (g *Generation) tagObservationEnvelope(obs ResourceObservation) (facts.Envelope, bool, error) {
	if g.key.IsZero() || !hasUsableTags(obs.Labels) {
		return facts.Envelope{}, false, nil
	}
	env, err := NewTagObservationEnvelope(TagObservation{
		Boundary:         g.boundary,
		FullResourceName: obs.Name,
		AssetType:        obs.AssetType,
		Tags:             obs.Labels,
		SourceKind:       tagSourceKindLabel,
		UpdateTime:       obs.UpdateTime,
		SourceRecordID:   obs.SourceRecordID,
		SourceURI:        obs.SourceURI,
	}, g.key)
	if err != nil {
		return facts.Envelope{}, false, err
	}
	return env, true, nil
}

func (g *Generation) relationshipEnvelopes(obs ResourceObservation) ([]facts.Envelope, error) {
	if len(obs.Relationships) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, len(obs.Relationships))
	for _, rel := range obs.Relationships {
		rel = relationshipWithResourceDefaults(rel, obs)
		if !hasUsableRelationshipObservation(rel) {
			continue
		}
		rel.Boundary = g.boundary
		rel.UpdateTime = obs.UpdateTime
		rel.SourceRecordID = relationshipSourceRecordID(
			obs.SourceRecordID,
			rel.RelationshipType,
			rel.TargetFullResourceName,
		)
		rel.SourceURI = obs.SourceURI
		env, err := NewCloudRelationshipEnvelope(rel)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	sort.Slice(envelopes, func(i, j int) bool {
		return envelopes[i].StableFactKey < envelopes[j].StableFactKey
	})
	return envelopes, nil
}

func (g *Generation) iamPolicyObservationEnvelopes(obs ResourceObservation) ([]facts.Envelope, error) {
	if g.key.IsZero() || iamPolicyObservationCount(obs.IAMPolicyBindings) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, iamPolicyObservationCount(obs.IAMPolicyBindings))
	for _, binding := range obs.IAMPolicyBindings {
		if !hasUsableIAMPolicyBinding(binding) {
			continue
		}
		env, err := NewIAMPolicyObservationEnvelope(IAMPolicyObservation{
			Boundary:                  g.boundary,
			FullResourceName:          obs.Name,
			AssetType:                 obs.AssetType,
			Role:                      binding.Role,
			Members:                   binding.Members,
			ConditionPresent:          binding.ConditionPresent,
			ConditionFingerprintInput: binding.ConditionFingerprintInput,
			Etag:                      binding.Etag,
			UpdateTime:                obs.UpdateTime,
			SourceRecordID:            iamSourceRecordID(obs.SourceRecordID, binding.Role),
			SourceURI:                 obs.SourceURI,
		}, g.key)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	sort.Slice(envelopes, func(i, j int) bool {
		return envelopes[i].StableFactKey < envelopes[j].StableFactKey
	})
	return envelopes, nil
}

func (g *Generation) dnsRecordEnvelopes(obs ResourceObservation) ([]facts.Envelope, error) {
	if g.key.IsZero() || dnsRecordObservationCount(obs.DNSRecords) == 0 {
		return nil, nil
	}
	envelopes := make([]facts.Envelope, 0, dnsRecordObservationCount(obs.DNSRecords))
	for _, record := range obs.DNSRecords {
		if !hasUsableDNSRecordObservation(record) {
			continue
		}
		record.Boundary = g.boundary
		record.UpdateTime = obs.UpdateTime
		record.SourceRecordID = dnsSourceRecordID(obs.SourceRecordID, record.RecordType, record.RecordName, g.key)
		record.SourceURI = obs.SourceURI
		env, err := NewDNSRecordEnvelope(record, g.key)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, env)
	}
	sort.Slice(envelopes, func(i, j int) bool {
		return envelopes[i].StableFactKey < envelopes[j].StableFactKey
	})
	return envelopes, nil
}

func hasUsableTags(labels map[string]string) bool {
	for key := range labels {
		if strings.TrimSpace(key) != "" {
			return true
		}
	}
	return false
}

func hasUsableRelationshipObservation(rel RelationshipObservation) bool {
	return strings.TrimSpace(rel.SourceFullResourceName) != "" &&
		strings.TrimSpace(rel.TargetFullResourceName) != "" &&
		strings.TrimSpace(rel.RelationshipType) != ""
}

func iamPolicyObservationCount(bindings []IAMPolicyBindingObservation) int {
	count := 0
	for _, binding := range bindings {
		if hasUsableIAMPolicyBinding(binding) {
			count++
		}
	}
	return count
}

func hasUsableIAMPolicyBinding(binding IAMPolicyBindingObservation) bool {
	if strings.TrimSpace(binding.Role) == "" {
		return false
	}
	for _, member := range binding.Members {
		if strings.TrimSpace(member) != "" {
			return true
		}
	}
	return false
}

func dnsRecordObservationCount(records []DNSRecordObservation) int {
	count := 0
	for _, record := range records {
		if hasUsableDNSRecordObservation(record) {
			count++
		}
	}
	return count
}

func hasUsableDNSRecordObservation(record DNSRecordObservation) bool {
	return strings.TrimSpace(record.ManagedZoneFullResourceName) != "" &&
		strings.TrimSpace(record.RecordType) != "" &&
		strings.TrimSpace(record.RecordName) != ""
}

func iamSourceRecordID(sourceRecordID, role string) string {
	sourceRecordID = strings.TrimSpace(sourceRecordID)
	role = strings.TrimSpace(role)
	if sourceRecordID == "" || role == "" {
		return sourceRecordID
	}
	return sourceRecordID + "|" + role
}

func relationshipWithResourceDefaults(rel RelationshipObservation, obs ResourceObservation) RelationshipObservation {
	if strings.TrimSpace(rel.SourceFullResourceName) == "" {
		rel.SourceFullResourceName = obs.Name
	}
	if strings.TrimSpace(rel.SourceAssetType) == "" {
		rel.SourceAssetType = obs.AssetType
	}
	return rel
}

func relationshipSourceRecordID(sourceRecordID, relationshipType, targetFullResourceName string) string {
	sourceRecordID = strings.TrimSpace(sourceRecordID)
	relationshipType = strings.TrimSpace(relationshipType)
	targetFullResourceName = strings.TrimSpace(targetFullResourceName)
	if sourceRecordID == "" || relationshipType == "" || targetFullResourceName == "" {
		return sourceRecordID
	}
	return sourceRecordID + "|" + relationshipType + "|" + targetFullResourceName
}

func dnsSourceRecordID(sourceRecordID, recordType, recordName string, key redact.Key) string {
	sourceRecordID = strings.TrimSpace(sourceRecordID)
	recordType = strings.ToUpper(strings.TrimSpace(recordType))
	recordName = strings.TrimSpace(recordName)
	if sourceRecordID == "" {
		if recordType == "" || recordName == "" || key.IsZero() {
			return sourceRecordID
		}
		return recordType + "|" + dnsRecordNameFingerprint(recordName, recordType, key)
	}
	if recordType == "" || recordName == "" || key.IsZero() {
		return sourceRecordID
	}
	return sourceRecordID + "|" + recordType + "|" + dnsRecordNameFingerprint(recordName, recordType, key)
}

func resourceDedupeKey(obs ResourceObservation) string {
	name := obs.Name
	if name == "" {
		return ""
	}
	return name + "\x00" + obs.AssetType
}

// GenerationTracker enforces per-scope fencing so a stale generation cannot
// replace current facts. It records the highest accepted fencing token per
// scope and rejects any lower token with ErrStaleGeneration while allowing
// idempotent re-acceptance of the current token. It is safe for concurrent use.
type GenerationTracker struct {
	mu      sync.Mutex
	current map[string]int64
}

// NewGenerationTracker creates an empty generation tracker.
func NewGenerationTracker() *GenerationTracker {
	return &GenerationTracker{current: make(map[string]int64)}
}

// Accept records an attempt to commit a generation for a scope at a fencing
// token. It returns ErrStaleGeneration when a strictly higher token already owns
// the scope. Re-accepting the current token is idempotent; a higher token
// advances the scope. The generationID is accepted for caller diagnostics and
// future per-generation tracking; fencing is by token.
func (t *GenerationTracker) Accept(scopeID, generationID string, fencingToken int64) error {
	_ = generationID
	t.mu.Lock()
	defer t.mu.Unlock()
	if current, ok := t.current[scopeID]; ok && fencingToken < current {
		return ErrStaleGeneration
	}
	t.current[scopeID] = fencingToken
	return nil
}

// Current returns the highest accepted fencing token for a scope and whether the
// scope has been seen.
func (t *GenerationTracker) Current(scopeID string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	token, ok := t.current[scopeID]
	return token, ok
}
