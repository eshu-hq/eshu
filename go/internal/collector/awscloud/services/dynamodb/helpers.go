package dynamodb

import (
	"strings"
	"time"
)

func keySchemaMaps(elements []KeySchemaElement) []map[string]string {
	if len(elements) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(elements))
	for _, element := range elements {
		name := strings.TrimSpace(element.AttributeName)
		if name == "" {
			continue
		}
		output = append(output, map[string]string{
			"attribute_name": name,
			"key_type":       strings.TrimSpace(element.KeyType),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func attributeDefinitionMaps(definitions []AttributeDefinition) []map[string]string {
	if len(definitions) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(definition.AttributeName)
		if name == "" {
			continue
		}
		output = append(output, map[string]string{
			"attribute_name": name,
			"attribute_type": strings.TrimSpace(definition.AttributeType),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func secondaryIndexMaps(indexes []SecondaryIndex) []map[string]any {
	if len(indexes) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(indexes))
	for _, index := range indexes {
		name := strings.TrimSpace(index.Name)
		if name == "" {
			continue
		}
		output = append(output, map[string]any{
			"name":                   name,
			"arn":                    strings.TrimSpace(index.ARN),
			"status":                 strings.TrimSpace(index.Status),
			"item_count":             index.ItemCount,
			"size_bytes":             index.SizeBytes,
			"backfilling":            index.Backfilling,
			"key_schema":             keySchemaMaps(index.KeySchema),
			"projection_type":        strings.TrimSpace(index.ProjectionType),
			"non_key_attributes":     cloneStrings(index.NonKeyAttributes),
			"provisioned_throughput": throughputMap(index.ProvisionedThroughput),
			"on_demand_throughput":   onDemandThroughputMap(index.OnDemandThroughput),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func replicaMaps(replicas []Replica) []map[string]string {
	if len(replicas) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(replicas))
	for _, replica := range replicas {
		region := strings.TrimSpace(replica.RegionName)
		if region == "" {
			continue
		}
		output = append(output, map[string]string{
			"region_name":       region,
			"status":            strings.TrimSpace(replica.Status),
			"kms_master_key_id": strings.TrimSpace(replica.KMSMasterKeyID),
			"table_class":       strings.TrimSpace(replica.TableClass),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func throughputMap(throughput Throughput) map[string]any {
	if throughput.ReadCapacityUnits == 0 &&
		throughput.WriteCapacityUnits == 0 &&
		throughput.NumberOfDecreasesToday == 0 {
		return nil
	}
	return map[string]any{
		"read_capacity_units":       throughput.ReadCapacityUnits,
		"write_capacity_units":      throughput.WriteCapacityUnits,
		"number_of_decreases_today": throughput.NumberOfDecreasesToday,
	}
}

func onDemandThroughputMap(throughput OnDemandThroughput) map[string]any {
	if throughput.MaxReadRequestUnits == 0 && throughput.MaxWriteRequestUnits == 0 {
		return nil
	}
	return map[string]any{
		"max_read_request_units":  throughput.MaxReadRequestUnits,
		"max_write_request_units": throughput.MaxWriteRequestUnits,
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
