// Package awssdk adapts the AWS SDK for Go v2 EventBridge client into the
// metadata-only EventBridge scanner interface.
//
// The adapter uses ListEventBuses, ListRules, DescribeRule, ListTargetsByRule,
// and ListTagsForResource. It intentionally excludes PutEvents, rule/target
// mutation calls, event bus policy persistence, and target payload fields such
// as Input, InputPath, InputTransformer, and HttpParameters.
package awssdk
