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
- **Counts reflect `main`.** As of this revision the collector ships **124
  service scanners**. The supported list below is grouped by domain; the
  scanner-coverage page enumerates the resources and edges each one emits.

## Supported services (124)

| Domain | Services |
|---|---|
| Compute & serverless (8) | `ec2`, `autoscaling`, `batch`, `lambda`, `lightsail`, `elasticbeanstalk`, `apprunner`, `imagebuilder` |
| Containers, registry & mesh (5) | `ecr`, `ecs`, `eks`, `appmesh`, `servicediscovery` |
| End-user computing (2) | `workspaces`, `appstream` |
| Storage & transfer (7) | `s3`, `efs`, `fsx`, `backup`, `storagegateway`, `datasync`, `transfer` |
| Databases (10) | `rds`, `dynamodb`, `elasticache`, `redshift`, `docdb`, `neptune`, `memorydb`, `keyspaces`, `timestream`, `dax` |
| Networking & content delivery (14) | `vpc`, `transitgateway`, `directconnect`, `globalaccelerator`, `route53`, `route53resolver`, `cloudfront`, `apigateway`, `apigatewayv2`, `elb`, `elbv2`, `networkfirewall`, `vpclattice`, `networkmanager` |
| Security, identity & compliance (27) | `iam`, `ssoadmin`, `cognito`, `ds`, `kms`, `acm`, `acmpca`, `secretsmanager`, `accessanalyzer`, `guardduty`, `inspector2`, `macie`, `securityhub`, `shield`, `wafv2`, `fms`, `detective`, `config`, `cloudtrail`, `ram`, `lakeformation`, `cloudhsmv2`, `auditmanager`, `signer`, `rolesanywhere`, `verifiedpermissions`, `verifiedaccess` |
| Management & governance (13) | `cloudformation`, `cloudwatch`, `cloudwatchlogs`, `ssm`, `organizations`, `servicecatalog`, `resourcegroups`, `controltower`, `servicequotas`, `computeoptimizer`, `licensemanager`, `proton`, `fis` |
| Developer tools (7) | `codebuild`, `codecommit`, `codedeploy`, `codepipeline`, `codeartifact`, `xray`, `codeguru` |
| Analytics & streaming (13) | `athena`, `emr`, `glue`, `kinesis`, `firehose`, `msk`, `opensearch`, `kinesisanalyticsv2`, `opensearchserverless`, `databrew`, `datazone`, `cleanrooms`, `quicksight` |
| Application integration (10) | `sns`, `sqs`, `eventbridge`, `stepfunctions`, `mq`, `appflow`, `appsync`, `mwaa`, `ses`, `pinpoint` |
| Observability & operations (2) | `grafana`, `amp` |
| ML & AI (2) | `sagemaker`, `bedrock` |
| Front-end, mobile & location (2) | `amplify`, `location` |
| Migration & edge (2) | `dms`, `outposts` |

## Not yet covered

The infrastructure-relevant surface is essentially complete. What remains is a
short tail of medium-value niche services plus the deliberately out-of-scope
leaf set.

### Candidate next wave (aws/J) — medium graph value

These own infrastructure resources with real edges into already-scanned
services. Tracked as issues under epic #51.

| Service | SDK / API | Graph value | Issue |
|---|---|---|---|
| Application Migration Service | `mgn` | source servers → launched EC2 instances, replication | #1000 |
| Elastic Disaster Recovery | `drs` | source servers → recovery EC2 instances, replication | #1001 |
| AppConfig | `appconfig` | apps / environments / profiles → Lambda / ECS, CloudWatch monitors | #1002 |
| Application Auto Scaling | `applicationautoscaling` | scalable targets → DynamoDB / ECS / Aurora | #1003 |
| Security Lake | `securitylake` | lake config, log sources, subscribers → S3 / Glue / KMS | #1004 |
| Service Catalog AppRegistry | `servicecatalogappregistry` | applications → associated resource collections | #1005 |
| Resilience Hub | `resiliencehub` | resilience apps → the resources they protect | #1006 |
| DocumentDB Elastic Clusters | `docdbelastic` | elastic clusters → VPC, KMS, Secrets Manager | #1007 |
| Route 53 ARC | `route53recoverycontrolconfig` | clusters, control panels, routing controls, readiness | #1008 |
| CloudWatch Synthetics | `synthetics` | canaries → S3, IAM, Lambda, VPC, monitored endpoints | #1009 |

EventBridge **Pipes / Scheduler / Schemas** are also candidates, better modeled
as an extension of the existing `eventbridge` scanner than as new services.

### Out of scope — leaf services, no infrastructure edges

Predominantly leaf services that add nodes but no graph edges; low priority by
design, not oversight:

- **AI/ML leaf APIs:** Comprehend, Rekognition, Polly, Transcribe, Translate,
  Textract, Lex, Kendra, Personalize, Forecast, Fraud Detector, Lookout\*,
  Amazon Q, HealthLake.
- **Media:** MediaConvert / MediaLive / MediaPackage / MediaStore /
  MediaConnect / MediaTailor, IVS, Elastic Transcoder.
- **IoT:** IoT Core, Greengrass, IoT Analytics, SiteWise, TwinMaker, IoT Events,
  FleetWise, Wireless.
- **Business applications:** Connect, Chime, WorkMail, WorkDocs, Supply Chain.
- **Edge / specialized compute:** Snow Family, Braket, Ground Station, Deadline
  Cloud, Panorama, Private 5G.
- **Cost management:** Cost Explorer, Budgets, Cost and Usage Report, Billing
  Conductor — billing surfaces with no graph resources.

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
