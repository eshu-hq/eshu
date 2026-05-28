// Package bindings imports every AWS service runtimebind so the production
// scanner registry is populated through init side effects.
//
// Importing this package once (typically from the collector-aws-cloud main)
// is the only step required to make the full set of AWS scanners available
// through awsruntime.DefaultScannerFactory. Adding a new scanner appends one
// blank import below; no other file in this package changes.
package bindings

import (
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/accessanalyzer/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/acm/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigateway/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/athena/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/backup/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudformation/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudfront/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudtrail/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatch/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatchlogs/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/docdb/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dynamodb/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ec2/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecs/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/efs/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eks/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elasticache/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eventbridge/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/glue/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/guardduty/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kinesis/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/inspector2/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kms/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mq/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/msk/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/organizations/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/rds/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/redshift/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sagemaker/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/secretsmanager/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securityhub/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sns/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sqs/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssoadmin/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssm/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/stepfunctions/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/transitgateway/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/vpc/runtimebind"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/wafv2/runtimebind"
)
