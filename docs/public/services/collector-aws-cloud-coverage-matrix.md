# AWS Collector Coverage Matrix

This page is the at-a-glance map of which AWS services the cloud collector
**scans today** versus which are **not yet covered**. It complements
[AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md), which is the
authoritative per-resource, per-relationship reference for every supported
scanner. Use that page for resource and edge detail; use this page to answer
"is service X covered, and if not, where does it sit on the roadmap?"

## Scope and method

- **Metadata-only.** Every scanner reads control-plane describe/list APIs and
  emits identity, configuration, and relationship metadata. Scanners never read
  data-plane payloads (object bodies, records, messages, secret values, model
  inputs).
- **Graph relevance drives priority.** A service earns a scanner when it owns
  infrastructure resources that form edges in the code-to-cloud graph (network
  placement, encryption keys, IAM trust, data-source wiring, deployment
  targets). Leaf services with no infrastructure relationships are low priority
  by design, not by oversight.
- **Counts reflect `main`.** As of this revision the collector ships **94
  service scanners**. The supported list below is grouped by domain; the
  scanner-coverage page enumerates the resources and edges each one emits.

## Supported services (94)

| Domain | Services |
|---|---|
| Compute & serverless (7) | `ec2`, `autoscaling`, `batch`, `lambda`, `lightsail`, `elasticbeanstalk`, `apprunner` |
| Containers, registry & mesh (5) | `ecr`, `ecs`, `eks`, `appmesh`, `servicediscovery` |
| Storage & transfer (7) | `s3`, `efs`, `fsx`, `backup`, `storagegateway`, `datasync`, `transfer` |
| Databases (9) | `rds`, `dynamodb`, `elasticache`, `redshift`, `docdb`, `neptune`, `memorydb`, `keyspaces`, `timestream` |
| Networking & content delivery (12) | `vpc`, `transitgateway`, `directconnect`, `globalaccelerator`, `route53`, `route53resolver`, `cloudfront`, `apigateway`, `apigatewayv2`, `elb`, `elbv2`, `networkfirewall` |
| Security, identity & compliance (21) | `iam`, `ssoadmin`, `cognito`, `ds`, `kms`, `acm`, `acmpca`, `secretsmanager`, `accessanalyzer`, `guardduty`, `inspector2`, `macie`, `securityhub`, `shield`, `wafv2`, `fms`, `detective`, `config`, `cloudtrail`, `ram`, `lakeformation` |
| Management & governance (8) | `cloudformation`, `cloudwatch`, `cloudwatchlogs`, `ssm`, `organizations`, `servicecatalog`, `resourcegroups`, `fis` |
| Developer tools (6) | `codebuild`, `codecommit`, `codedeploy`, `codepipeline`, `codeartifact`, `xray` |
| Analytics & streaming (7) | `athena`, `emr`, `glue`, `kinesis`, `firehose`, `msk`, `opensearch` |
| Application integration (9) | `sns`, `sqs`, `eventbridge`, `stepfunctions`, `mq`, `appflow`, `appsync`, `mwaa`, `ses` |
| ML & front-end (3) | `sagemaker`, `bedrock`, `amplify` |

## Not yet covered

### Tier A — high graph value, commonly deployed

These own infrastructure resources with strong edges into already-scanned
services. They are the recommended next expansion wave.

| Service | SDK / API | Graph value |
|---|---|---|
| Database Migration Service | `dms` | replication instances live in a VPC; endpoints fan out to RDS / S3 / Kinesis / Redshift |
| Managed Service for Apache Flink | `kinesisanalyticsv2` | applications → Kinesis / MSK / S3 sources and sinks |
| VPC Lattice | `vpc-lattice` | service networks, services, target groups → VPC / EC2 / Lambda |
| Network Manager | `networkmanager` | global networks → transit gateways, Direct Connect |
| DynamoDB Accelerator | `dax` | clusters → DynamoDB tables, VPC subnets/security groups |
| CloudHSM v2 | `cloudhsmv2` | clusters → VPC, KMS custom key stores |
| EC2 Image Builder | `imagebuilder` | pipelines / recipes → AMIs, ECR, distribution configs |
| WorkSpaces | `workspaces` | directories, bundles → Directory Service, VPC |
| AppStream 2.0 | `appstream` | fleets, stacks, image builders → VPC, IAM |
| QuickSight | `quicksight` | data sources → Redshift / Athena / RDS / S3 |
| Outposts | `outposts` | sites, racks, outposts → EC2 / VPC placement |

### Tier B — moderate graph value (governance, security, dev-tools)

Worth covering after Tier A; mostly thinner resource graphs or narrower
deployment footprints.

| Service | SDK / API |
|---|---|
| Service Quotas | `service-quotas` |
| Control Tower | `controltower` |
| Proton | `proton` |
| License Manager | `license-manager` |
| Compute Optimizer | `compute-optimizer` |
| Audit Manager | `auditmanager` |
| Signer | `signer` |
| IAM Roles Anywhere | `rolesanywhere` |
| Verified Permissions | `verifiedpermissions` |
| EC2 Verified Access | `ec2` (verified-access) |
| CodeGuru (Reviewer + Profiler) | `codeguru-reviewer`, `codeguruprofiler` |
| Glue DataBrew | `databrew` |
| DataZone | `datazone` |
| Clean Rooms | `cleanrooms` |
| OpenSearch Serverless / Ingestion | `opensearchserverless`, `osis` |
| Managed Grafana | `grafana` |
| Managed Service for Prometheus | `amp` |
| Pinpoint | `pinpoint` |
| Location Service | `location` |

### Tier C — low graph value (out of scope unless a use case appears)

These are predominantly leaf services with no infrastructure relationships, so
they add nodes but few edges. They are intentionally deprioritized:

- **AI/ML leaf APIs:** Comprehend, Rekognition, Polly, Transcribe, Translate,
  Textract, Lex, Kendra, Personalize, Forecast, Fraud Detector, Lookout\*, Q.
- **Media:** MediaConvert / MediaLive / MediaPackage / MediaStore /
  MediaConnect / MediaTailor, Elemental, IVS.
- **IoT:** IoT Core, Greengrass, IoT Analytics, SiteWise, TwinMaker, IoT Events.
- **Business applications:** Connect, Chime, WorkMail, WorkDocs, Supply Chain.
- **Edge / specialized compute:** Snow Family, Braket, Ground Station, Deadline,
  RoboMaker.
- **Cost management:** Cost Explorer, Budgets, Cost and Usage Report — billing
  surfaces with no graph resources.

## In-scanner sub-resource gaps

These are gaps *inside* an already-shipped scanner rather than missing services:

- **Kinesis Video Streams — covered; only WebRTC signaling channels are not
  modeled.** Epic #750 scoped the `kinesis` scanner as "Data Streams + Firehose
  + Video Streams." All three shipped: Data Streams and Video Streams under the
  `kinesis` scanner (`aws_kinesis_video_stream` with a stream → `aws_kms_key`
  edge, from #798), and Firehose as its own `firehose` scanner. The only KVS
  sub-feature not modeled is WebRTC **signaling channels**
  (`ListSignalingChannels`), which are identity-only with no cross-service edges
  — deliberately out of scope as low graph value. (Issue #915, which assumed
  video streams were unshipped, was closed as already-satisfied.)

## Deeper coverage track

Separate from "missing services," epic #51 carries a *deeper coverage* track:
expanding existing scanners past their MVP metadata surface — for example S3
inventory integration, EC2 deep configuration, and RDS performance insights.
That work deepens nodes already in the graph and is tracked independently of new
service onboarding.

## Maintenance

When a new scanner merges, add its service to the supported table here and to
[AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md), and remove it
from the Tier tables above. The supported count in
[Scope and method](#scope-and-method) must match the number of scanner
directories under `go/internal/collector/awscloud/services/`.
