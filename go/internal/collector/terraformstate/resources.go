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
		if err := p.checkContext(); err != nil {
			return err
		}
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
		if err := p.checkContext(); err != nil {
			return err
		}
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
		if err := p.checkContext(); err != nil {
			return err
		}
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
		if err := p.checkContext(); err != nil {
			return err
		}
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
	if err := p.emitTagObservations(address, attributes); err != nil {
		return err
	}
	classifiedAttributes, err := p.classifyAttributes(strings.TrimSpace(resource.Type), address, attributes)
	if err != nil {
		return err
	}
	return p.emitResourceInstance(resource, instance, instanceIndex, classifiedAttributes, p.correlationAnchors(strings.TrimSpace(resource.Type), address, attributes))
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
	if err := p.emitModuleObservation(resource.Module, address); err != nil {
		return err
	}
	if err := p.emitProviderBinding(address, resource.Provider); err != nil {
		return err
	}
	if err := p.emitBodyFact(p.envelope(facts.TerraformStateResourceFactKind, "resource:"+address, payload, address)); err != nil {
		return err
	}
	p.resourceFacts++
	return nil
}
