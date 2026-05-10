package terraformstate

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type resourceContext struct {
	Mode     string
	Type     string
	Name     string
	Module   string
	Provider string
}

type instanceContext struct {
	HasIndexKey  bool
	IndexKeyHash string
}

func (p *stateParser) readResources() error {
	if err := readOpeningDelim(p.decoder, '[', "terraform state resources"); err != nil {
		return err
	}
	for index := 0; p.decoder.More(); index++ {
		if err := p.readResource(index); err != nil {
			return err
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resources: %w", err)
	}
	return nil
}

func (p *stateParser) readResource(resourceIndex int) error {
	resource := resourceContext{}
	sawInstances := false
	if err := readOpeningDelim(p.decoder, '{', fmt.Sprintf("terraform state resource %d", resourceIndex)); err != nil {
		return err
	}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return fmt.Errorf("read terraform state resource %d key: %w", resourceIndex, err)
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("terraform state resource %d key must be a string", resourceIndex)
		}
		switch key {
		case "mode":
			resource.Mode, err = readString(p.decoder, "terraform state resource mode")
		case "type":
			resource.Type, err = readString(p.decoder, "terraform state resource type")
		case "name":
			resource.Name, err = readString(p.decoder, "terraform state resource name")
		case "module":
			resource.Module, err = readString(p.decoder, "terraform state resource module")
		case "provider":
			resource.Provider, err = readString(p.decoder, "terraform state resource provider")
		case "instances":
			if err := validateResourceIdentity(resource); err != nil {
				return fmt.Errorf("terraform state resource %d identity before instances: %w", resourceIndex, err)
			}
			sawInstances = true
			err = p.readInstances(resource)
		default:
			err = skipValue(p.decoder)
		}
		if err != nil {
			return err
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resource %d: %w", resourceIndex, err)
	}
	if !sawInstances {
		return p.emitResourceInstance(resource, instanceContext{}, 0, map[string]any{}, nil)
	}
	return nil
}

func (p *stateParser) readInstances(resource resourceContext) error {
	if err := readOpeningDelim(p.decoder, '[', "terraform state resource instances"); err != nil {
		return err
	}
	count := 0
	for ; p.decoder.More(); count++ {
		if err := p.readInstance(resource, count); err != nil {
			return err
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resource instances: %w", err)
	}
	if count == 0 {
		return p.emitResourceInstance(resource, instanceContext{}, 0, map[string]any{}, nil)
	}
	return nil
}

func (p *stateParser) readInstance(resource resourceContext, instanceIndex int) error {
	instance := instanceContext{}
	attributes := []attributeValue{}
	if err := readOpeningDelim(p.decoder, '{', "terraform state resource instance"); err != nil {
		return err
	}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return fmt.Errorf("read terraform state resource instance key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("terraform state resource instance key must be a string")
		}
		switch key {
		case "index_key":
			value, scalar, err := readScalarOrSkip(p.decoder)
			if err != nil {
				return fmt.Errorf("decode terraform state instance index key: %w", err)
			}
			if scalar {
				instance.HasIndexKey = true
				instance.IndexKeyHash = instanceIndexHash(value)
			}
		case "attributes":
			readAttributes, err := readAttributeValues(p.decoder)
			if err != nil {
				return err
			}
			attributes = readAttributes
		default:
			if err := skipValue(p.decoder); err != nil {
				return err
			}
		}
	}
	if _, err := p.decoder.Token(); err != nil {
		return fmt.Errorf("close terraform state resource instance: %w", err)
	}
	address := resourceAddress(resource, instance, instanceIndex)
	p.emitTagObservations(address, attributes)
	return p.emitResourceInstance(resource, instance, instanceIndex, p.classifyAttributes(address, attributes), p.correlationAnchors(address, attributes))
}

func (p *stateParser) emitResourceInstance(resource resourceContext, instance instanceContext, instanceIndex int, attributes map[string]any, anchors []any) error {
	if err := validateResourceIdentity(resource); err != nil {
		return err
	}
	address := resourceAddress(resource, instance, instanceIndex)
	payload := map[string]any{
		"address":    address,
		"mode":       strings.TrimSpace(resource.Mode),
		"type":       strings.TrimSpace(resource.Type),
		"name":       strings.TrimSpace(resource.Name),
		"module":     strings.TrimSpace(resource.Module),
		"provider":   strings.TrimSpace(resource.Provider),
		"attributes": attributes,
	}
	if len(anchors) > 0 {
		payload["correlation_anchors"] = anchors
	}
	p.recordModuleObservation(resource.Module)
	p.recordProviderBinding(address, resource.Provider)
	p.facts = append(p.facts, p.envelope(facts.TerraformStateResourceFactKind, "resource:"+address, payload, address))
	p.resourceFacts++
	return nil
}
