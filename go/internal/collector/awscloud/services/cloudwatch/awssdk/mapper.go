// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	awscwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	cwservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatch"
)

func mapMetricAlarm(raw awscwtypes.MetricAlarm, tags map[string]string) cwservice.MetricAlarm {
	return cwservice.MetricAlarm{
		ARN:                                aws.ToString(raw.AlarmArn),
		Name:                               aws.ToString(raw.AlarmName),
		Description:                        aws.ToString(raw.AlarmDescription),
		State:                              string(raw.StateValue),
		StateReason:                        aws.ToString(raw.StateReason),
		ActionsEnabled:                     aws.ToBool(raw.ActionsEnabled),
		AlarmActions:                       cloneStrings(raw.AlarmActions),
		OKActions:                          cloneStrings(raw.OKActions),
		InsufficientDataActions:            cloneStrings(raw.InsufficientDataActions),
		Namespace:                          aws.ToString(raw.Namespace),
		MetricName:                         aws.ToString(raw.MetricName),
		Statistic:                          string(raw.Statistic),
		ExtendedStatistic:                  aws.ToString(raw.ExtendedStatistic),
		ComparisonOperator:                 string(raw.ComparisonOperator),
		Threshold:                          raw.Threshold,
		EvaluationPeriods:                  aws.ToInt32(raw.EvaluationPeriods),
		DatapointsToAlarm:                  aws.ToInt32(raw.DatapointsToAlarm),
		Period:                             aws.ToInt32(raw.Period),
		TreatMissingData:                   aws.ToString(raw.TreatMissingData),
		EvaluateLowSampleCountPercentile:   aws.ToString(raw.EvaluateLowSampleCountPercentile),
		Unit:                               string(raw.Unit),
		Dimensions:                         mapDimensions(raw.Dimensions),
		StateUpdatedTimestamp:              aws.ToTime(raw.StateUpdatedTimestamp),
		AlarmConfigurationUpdatedTimestamp: aws.ToTime(raw.AlarmConfigurationUpdatedTimestamp),
		Tags:                               tags,
	}
}

func mapCompositeAlarm(raw awscwtypes.CompositeAlarm, tags map[string]string) cwservice.CompositeAlarm {
	rule := aws.ToString(raw.AlarmRule)
	return cwservice.CompositeAlarm{
		ARN:                                aws.ToString(raw.AlarmArn),
		Name:                               aws.ToString(raw.AlarmName),
		Description:                        aws.ToString(raw.AlarmDescription),
		State:                              string(raw.StateValue),
		StateReason:                        aws.ToString(raw.StateReason),
		ActionsEnabled:                     aws.ToBool(raw.ActionsEnabled),
		AlarmRule:                          rule,
		AlarmActions:                       cloneStrings(raw.AlarmActions),
		OKActions:                          cloneStrings(raw.OKActions),
		InsufficientDataActions:            cloneStrings(raw.InsufficientDataActions),
		ChildAlarmNames:                    extractChildAlarmNames(rule),
		StateUpdatedTimestamp:              aws.ToTime(raw.StateUpdatedTimestamp),
		AlarmConfigurationUpdatedTimestamp: aws.ToTime(raw.AlarmConfigurationUpdatedTimestamp),
		Tags:                               tags,
	}
}

func mapDashboard(raw awscwtypes.DashboardEntry) cwservice.Dashboard {
	// NEVER persist the dashboard body JSON. The raw type does not expose it
	// here (ListDashboards only returns identity), and the adapter does not
	// call GetDashboard. This mapper exists to make the exclusion explicit.
	return cwservice.Dashboard{
		ARN:          aws.ToString(raw.DashboardArn),
		Name:         aws.ToString(raw.DashboardName),
		LastModified: aws.ToTime(raw.LastModified),
		SizeInBytes:  aws.ToInt64(raw.Size),
	}
}

func mapInsightRule(raw awscwtypes.InsightRule) cwservice.InsightRule {
	// NEVER persist raw.Definition. The Contributor Insights rule definition
	// is a SQL-like grammar that may encode customer query patterns.
	return cwservice.InsightRule{
		Name:   aws.ToString(raw.Name),
		State:  aws.ToString(raw.State),
		Schema: aws.ToString(raw.Schema),
	}
}

func mapMetricStream(
	entry awscwtypes.MetricStreamEntry,
	details *awscw.GetMetricStreamOutput,
	tags map[string]string,
) cwservice.MetricStream {
	stream := cwservice.MetricStream{
		ARN:            aws.ToString(entry.Arn),
		Name:           aws.ToString(entry.Name),
		State:          aws.ToString(entry.State),
		OutputFormat:   string(entry.OutputFormat),
		FirehoseARN:    aws.ToString(entry.FirehoseArn),
		CreationDate:   aws.ToTime(entry.CreationDate),
		LastUpdateDate: aws.ToTime(entry.LastUpdateDate),
		Tags:           tags,
	}
	if details != nil {
		stream.RoleARN = aws.ToString(details.RoleArn)
		stream.IncludeLinkedAccount = aws.ToBool(details.IncludeLinkedAccountsMetrics)
		if details.FirehoseArn != nil {
			stream.FirehoseARN = aws.ToString(details.FirehoseArn)
		}
		if details.OutputFormat != "" {
			stream.OutputFormat = string(details.OutputFormat)
		}
		if details.State != nil {
			stream.State = aws.ToString(details.State)
		}
	}
	return stream
}

func mapDimensions(input []awscwtypes.Dimension) []cwservice.MetricDimension {
	if len(input) == 0 {
		return nil
	}
	output := make([]cwservice.MetricDimension, 0, len(input))
	for _, dim := range input {
		name := strings.TrimSpace(aws.ToString(dim.Name))
		if name == "" {
			continue
		}
		output = append(output, cwservice.MetricDimension{
			Name:  name,
			Value: aws.ToString(dim.Value),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// childAlarmPattern matches ALARM("name"), OK("name"), INSUFFICIENT_DATA("name"),
// and the unquoted ALARM(name) form used in CloudWatch composite alarm rules.
var childAlarmPattern = regexp.MustCompile(`(?:ALARM|OK|INSUFFICIENT_DATA)\s*\(\s*"?([^")]+)"?\s*\)`)

// extractChildAlarmNames returns the unique alarm names referenced by a
// composite alarm's AlarmRule. The rule is not persisted as queryable graph
// truth; we only need its child references to materialize edges.
func extractChildAlarmNames(rule string) []string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return nil
	}
	matches := childAlarmPattern.FindAllStringSubmatch(rule, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
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
