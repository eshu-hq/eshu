package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsroute53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	route53service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53"
)

func mapHostedZone(hostedZone awsroute53types.HostedZone, tags map[string]string) route53service.HostedZone {
	value := route53service.HostedZone{
		ID:                     aws.ToString(hostedZone.Id),
		Name:                   aws.ToString(hostedZone.Name),
		CallerReference:        aws.ToString(hostedZone.CallerReference),
		ResourceRecordSetCount: aws.ToInt64(hostedZone.ResourceRecordSetCount),
		Tags:                   tags,
	}
	if hostedZone.Config != nil {
		value.Comment = aws.ToString(hostedZone.Config.Comment)
		value.Private = hostedZone.Config.PrivateZone
	}
	if hostedZone.LinkedService != nil {
		value.LinkedService = route53service.LinkedService{
			ServicePrincipal: aws.ToString(hostedZone.LinkedService.ServicePrincipal),
			Description:      aws.ToString(hostedZone.LinkedService.Description),
		}
	}
	return value
}

func mapRecordSet(record awsroute53types.ResourceRecordSet) route53service.RecordSet {
	return route53service.RecordSet{
		Name:                    aws.ToString(record.Name),
		Type:                    string(record.Type),
		SetIdentifier:           aws.ToString(record.SetIdentifier),
		TTL:                     record.TTL,
		Values:                  mapValues(record.ResourceRecords),
		AliasTarget:             mapAliasTarget(record.AliasTarget),
		Weight:                  record.Weight,
		Region:                  string(record.Region),
		Failover:                string(record.Failover),
		HealthCheckID:           aws.ToString(record.HealthCheckId),
		MultiValueAnswer:        record.MultiValueAnswer,
		TrafficPolicyInstanceID: aws.ToString(record.TrafficPolicyInstanceId),
		GeoLocation:             mapGeoLocation(record.GeoLocation),
		CIDRRouting:             mapCIDRRouting(record.CidrRoutingConfig),
	}
}

func mapAliasTarget(input *awsroute53types.AliasTarget) *route53service.AliasTarget {
	if input == nil {
		return nil
	}
	return &route53service.AliasTarget{
		DNSName:              aws.ToString(input.DNSName),
		HostedZoneID:         aws.ToString(input.HostedZoneId),
		EvaluateTargetHealth: input.EvaluateTargetHealth,
	}
}

func mapGeoLocation(input *awsroute53types.GeoLocation) route53service.GeoLocation {
	if input == nil {
		return route53service.GeoLocation{}
	}
	return route53service.GeoLocation{
		ContinentCode:   aws.ToString(input.ContinentCode),
		CountryCode:     aws.ToString(input.CountryCode),
		SubdivisionCode: aws.ToString(input.SubdivisionCode),
	}
}

func mapCIDRRouting(input *awsroute53types.CidrRoutingConfig) route53service.CIDRRouting {
	if input == nil {
		return route53service.CIDRRouting{}
	}
	return route53service.CIDRRouting{
		CollectionID: aws.ToString(input.CollectionId),
		LocationName: aws.ToString(input.LocationName),
	}
}

func mapValues(records []awsroute53types.ResourceRecord) []string {
	if len(records) == 0 {
		return nil
	}
	values := make([]string, 0, len(records))
	for _, record := range records {
		value := strings.TrimSpace(aws.ToString(record.Value))
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func mapTags(tags []awsroute53types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	return output
}

func trimHostedZonePrefix(hostedZoneID string) string {
	return strings.TrimPrefix(strings.TrimSpace(hostedZoneID), "/hostedzone/")
}
