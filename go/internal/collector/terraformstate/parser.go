package terraformstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type stateOutput struct {
	Sensitive bool `json:"sensitive"`
	Value     any  `json:"value"`
}

type stateResource struct {
	Mode      string          `json:"mode"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Module    string          `json:"module"`
	Provider  string          `json:"provider"`
	Instances []stateInstance `json:"instances"`
}

type stateInstance struct {
	IndexKey   any            `json:"index_key"`
	Attributes map[string]any `json:"attributes"`
}

type snapshotMetadata struct {
	FormatVersion    string
	TerraformVersion string
	Serial           int64
	Lineage          string
}

// Parse streams a Terraform state reader into redacted fact envelopes.
func Parse(ctx context.Context, reader io.Reader, options ParseOptions) (ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return ParseResult{}, err
	}
	if reader == nil {
		return ParseResult{}, fmt.Errorf("terraform state reader must not be nil")
	}
	if options.RedactionKey.IsZero() {
		return ParseResult{}, fmt.Errorf("terraform state redaction key must not be empty")
	}

	parser := stateParser{
		decoder: json.NewDecoder(reader),
		options: normalizedParseOptions(options),
	}
	if err := parser.parse(); err != nil {
		return ParseResult{}, err
	}
	return ParseResult{Facts: parser.facts}, nil
}

type stateParser struct {
	decoder  *json.Decoder
	options  ParseOptions
	snapshot snapshotMetadata
	facts    []facts.Envelope
	warnings []warningPayload
}

func (p *stateParser) parse() error {
	token, err := p.decoder.Token()
	if err != nil {
		return fmt.Errorf("read terraform state object: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("terraform state root must be an object")
	}

	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return fmt.Errorf("read terraform state key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("terraform state object key must be a string")
		}
		if err := p.readField(key); err != nil {
			return err
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state object: %w", err)
	}

	p.emitSnapshot()
	p.emitWarnings()
	return nil
}

func (p *stateParser) readField(key string) error {
	switch key {
	case "format_version":
		return p.decoder.Decode(&p.snapshot.FormatVersion)
	case "terraform_version":
		return p.decoder.Decode(&p.snapshot.TerraformVersion)
	case "serial":
		return p.decoder.Decode(&p.snapshot.Serial)
	case "lineage":
		return p.decoder.Decode(&p.snapshot.Lineage)
	case "outputs":
		return p.readOutputs()
	case "resources":
		return p.readResources()
	default:
		var discard any
		return p.decoder.Decode(&discard)
	}
}

func (p *stateParser) readOutputs() error {
	outputs := map[string]stateOutput{}
	if err := p.decoder.Decode(&outputs); err != nil {
		return fmt.Errorf("decode terraform state outputs: %w", err)
	}
	for name, output := range outputs {
		payload := map[string]any{
			"name":      name,
			"sensitive": output.Sensitive,
		}
		if output.Sensitive {
			payload["value"] = redactionMap(redact.Scalar(output.Value, "sensitive_output", "outputs."+name, p.options.RedactionKey))
		} else if isScalar(output.Value) {
			payload["value"] = output.Value
		} else {
			payload["value_shape"] = "composite"
		}
		p.facts = append(p.facts, p.envelope(facts.TerraformStateOutputFactKind, "output:"+name, payload, name))
	}
	return nil
}

func (p *stateParser) readResources() error {
	token, err := p.decoder.Token()
	if err != nil {
		return fmt.Errorf("read terraform state resources: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '[' {
		return fmt.Errorf("terraform state resources must be an array")
	}
	for index := 0; p.decoder.More(); index++ {
		var resource stateResource
		if err := p.decoder.Decode(&resource); err != nil {
			return fmt.Errorf("decode terraform state resource %d: %w", index, err)
		}
		p.emitResource(resource, index)
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resources: %w", err)
	}
	return nil
}

func (p *stateParser) emitResource(resource stateResource, resourceIndex int) {
	if len(resource.Instances) == 0 {
		p.emitResourceInstance(resource, resourceIndex, 0, stateInstance{})
		return
	}
	for instanceIndex, instance := range resource.Instances {
		p.emitResourceInstance(resource, resourceIndex, instanceIndex, instance)
	}
}

func (p *stateParser) emitResourceInstance(resource stateResource, resourceIndex int, instanceIndex int, instance stateInstance) {
	address := resourceAddress(resource, instance, instanceIndex)
	attributes := p.redactAttributes(address, instance.Attributes)
	payload := map[string]any{
		"address":    address,
		"mode":       strings.TrimSpace(resource.Mode),
		"type":       strings.TrimSpace(resource.Type),
		"name":       strings.TrimSpace(resource.Name),
		"module":     strings.TrimSpace(resource.Module),
		"provider":   strings.TrimSpace(resource.Provider),
		"attributes": attributes,
	}
	key := fmt.Sprintf("resource:%d:%d:%s", resourceIndex, instanceIndex, address)
	p.facts = append(p.facts, p.envelope(facts.TerraformStateResourceFactKind, key, payload, address))
}

func (p *stateParser) redactAttributes(address string, attributes map[string]any) map[string]any {
	redacted := make(map[string]any, len(attributes))
	for key, value := range attributes {
		source := "resources." + address + ".attributes." + key
		kind := redact.FieldScalar
		if !isScalar(value) {
			kind = redact.FieldComposite
		}
		decision := p.options.RedactionRules.Classify(source, redact.SchemaKnown, kind)
		if decision.Action == redact.ActionPreserve {
			decision = p.options.RedactionRules.Classify(source, redact.SchemaUnknown, kind)
		}

		switch decision.Action {
		case redact.ActionPreserve:
			redacted[key] = value
		case redact.ActionRedact:
			redacted[key] = redactionMap(redact.Scalar(value, decision.Reason, decision.Source, p.options.RedactionKey))
		case redact.ActionDrop:
			p.warnings = append(p.warnings, warningPayload{
				WarningKind: "attribute_dropped",
				Reason:      decision.Reason,
				Source:      decision.Source,
			})
		}
	}
	return redacted
}

func (p *stateParser) emitSnapshot() {
	payload := map[string]any{
		"format_version":    p.snapshot.FormatVersion,
		"terraform_version": p.snapshot.TerraformVersion,
		"serial":            p.snapshot.Serial,
		"lineage":           p.snapshot.Lineage,
		"backend_kind":      string(p.options.Source.BackendKind),
		"locator_hash":      locatorHash(p.options.Source),
		"source_size_bytes": p.options.Metadata.Size,
	}
	p.facts = append([]facts.Envelope{
		p.envelope(facts.TerraformStateSnapshotFactKind, "snapshot", payload, locatorHash(p.options.Source)),
	}, p.facts...)
}

func (p *stateParser) emitWarnings() {
	for index, warning := range p.warnings {
		payload := map[string]any{
			"warning_kind": warning.WarningKind,
			"reason":       warning.Reason,
			"source":       warning.Source,
		}
		key := fmt.Sprintf("warning:%d:%s:%s", index, warning.WarningKind, warning.Source)
		p.facts = append(p.facts, p.envelope(facts.TerraformStateWarningFactKind, key, payload, warning.Source))
	}
}

func (p *stateParser) envelope(kind string, stableKey string, payload map[string]any, sourceRecordID string) facts.Envelope {
	version, _ := facts.TerraformStateSchemaVersion(kind)
	key := kind + ":" + stableKey
	return facts.Envelope{
		FactID: facts.StableID("TerraformStateFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    key,
			"scope_id":      p.options.Scope.ScopeID,
			"generation_id": p.options.Generation.GenerationID,
		}),
		ScopeID:          p.options.Scope.ScopeID,
		GenerationID:     p.options.Generation.GenerationID,
		FactKind:         kind,
		StableFactKey:    key,
		SchemaVersion:    version,
		CollectorKind:    string(scope.CollectorTerraformState),
		FencingToken:     p.options.FencingToken,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       p.options.ObservedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   string(scope.CollectorTerraformState),
			ScopeID:        p.options.Scope.ScopeID,
			GenerationID:   p.options.Generation.GenerationID,
			FactKey:        key,
			SourceURI:      sourceURI(p.options.Source),
			SourceRecordID: sourceRecordID,
		},
	}
}

type warningPayload struct {
	WarningKind string
	Reason      string
	Source      string
}

func normalizedParseOptions(options ParseOptions) ParseOptions {
	if options.ObservedAt.IsZero() {
		options.ObservedAt = options.Generation.ObservedAt
	}
	if options.ObservedAt.IsZero() {
		options.ObservedAt = time.Now().UTC()
	}
	options.ObservedAt = options.ObservedAt.UTC()
	return options
}

func resourceAddress(resource stateResource, instance stateInstance, instanceIndex int) string {
	prefix := ""
	if module := strings.TrimSpace(resource.Module); module != "" {
		prefix = module + "."
	}
	address := prefix + strings.TrimSpace(resource.Type) + "." + strings.TrimSpace(resource.Name)
	if instance.IndexKey != nil {
		return fmt.Sprintf("%s[%v]", address, instance.IndexKey)
	}
	if instanceIndex > 0 {
		return fmt.Sprintf("%s[%d]", address, instanceIndex)
	}
	return address
}

func isScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, float64, int, int64, json.Number:
		return true
	default:
		return false
	}
}

func redactionMap(value redact.Value) map[string]any {
	return map[string]any{
		"marker": value.Marker,
		"reason": value.Reason,
		"source": value.Source,
	}
}

func sourceURI(source StateKey) string {
	return fmt.Sprintf("terraform_state:%s:%s", source.BackendKind, locatorHash(source))
}

func locatorHash(source StateKey) string {
	sum := sha256.Sum256([]byte(string(source.BackendKind) + "\x00" + source.Locator + "\x00" + source.VersionID))
	return hex.EncodeToString(sum[:])
}
