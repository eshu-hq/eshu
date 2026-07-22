module github.com/eshu-hq/eshu/go

go 1.26.5

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.17.0
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.8.1
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph v0.8.2
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources v1.2.0
	github.com/UserNobody14/tree-sitter-dart v0.0.0-20260520003023-a9bdfa3db2fb
	github.com/alexaandru/go-sitter-forest/perl v1.9.9
	github.com/alexaandru/go-sitter-forest/sql v1.9.9
	github.com/aws/aws-sdk-go-v2 v1.41.9
	github.com/aws/aws-sdk-go-v2/config v1.29.15
	github.com/aws/aws-sdk-go-v2/credentials v1.17.68
	github.com/aws/aws-sdk-go-v2/service/accessanalyzer v1.48.0
	github.com/aws/aws-sdk-go-v2/service/acm v1.39.0
	github.com/aws/aws-sdk-go-v2/service/acmpca v1.39.0
	github.com/aws/aws-sdk-go-v2/service/amp v1.43.1
	github.com/aws/aws-sdk-go-v2/service/amplify v1.38.18
	github.com/aws/aws-sdk-go-v2/service/apigateway v1.39.3
	github.com/aws/aws-sdk-go-v2/service/apigatewayv2 v1.34.3
	github.com/aws/aws-sdk-go-v2/service/appconfig v1.44.2
	github.com/aws/aws-sdk-go-v2/service/appflow v1.51.16
	github.com/aws/aws-sdk-go-v2/service/applicationautoscaling v1.41.18
	github.com/aws/aws-sdk-go-v2/service/appmesh v1.35.15
	github.com/aws/aws-sdk-go-v2/service/apprunner v1.40.1
	github.com/aws/aws-sdk-go-v2/service/appstream v1.60.1
	github.com/aws/aws-sdk-go-v2/service/appsync v1.53.8
	github.com/aws/aws-sdk-go-v2/service/athena v1.57.6
	github.com/aws/aws-sdk-go-v2/service/auditmanager v1.46.16
	github.com/aws/aws-sdk-go-v2/service/autoscaling v1.66.3
	github.com/aws/aws-sdk-go-v2/service/backup v1.57.0
	github.com/aws/aws-sdk-go-v2/service/batch v1.65.1
	github.com/aws/aws-sdk-go-v2/service/bedrock v1.62.0
	github.com/aws/aws-sdk-go-v2/service/bedrockagent v1.54.1
	github.com/aws/aws-sdk-go-v2/service/cleanrooms v1.45.2
	github.com/aws/aws-sdk-go-v2/service/cloudformation v1.55.0
	github.com/aws/aws-sdk-go-v2/service/cloudfront v1.64.0
	github.com/aws/aws-sdk-go-v2/service/cloudhsmv2 v1.35.0
	github.com/aws/aws-sdk-go-v2/service/cloudtrail v1.55.11
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.57.0
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.73.0
	github.com/aws/aws-sdk-go-v2/service/codeartifact v1.39.2
	github.com/aws/aws-sdk-go-v2/service/codebuild v1.68.17
	github.com/aws/aws-sdk-go-v2/service/codecommit v1.33.0
	github.com/aws/aws-sdk-go-v2/service/codedeploy v1.35.16
	github.com/aws/aws-sdk-go-v2/service/codeguruprofiler v1.30.2
	github.com/aws/aws-sdk-go-v2/service/codegurureviewer v1.35.1
	github.com/aws/aws-sdk-go-v2/service/codepipeline v1.46.24
	github.com/aws/aws-sdk-go-v2/service/cognitoidentity v1.33.25
	github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider v1.60.3
	github.com/aws/aws-sdk-go-v2/service/computeoptimizer v1.51.2
	github.com/aws/aws-sdk-go-v2/service/configservice v1.48.1
	github.com/aws/aws-sdk-go-v2/service/controltower v1.29.2
	github.com/aws/aws-sdk-go-v2/service/databasemigrationservice v1.63.2
	github.com/aws/aws-sdk-go-v2/service/databrew v1.40.2
	github.com/aws/aws-sdk-go-v2/service/datasync v1.59.2
	github.com/aws/aws-sdk-go-v2/service/datazone v1.62.2
	github.com/aws/aws-sdk-go-v2/service/dax v1.29.20
	github.com/aws/aws-sdk-go-v2/service/detective v1.39.1
	github.com/aws/aws-sdk-go-v2/service/directconnect v1.38.18
	github.com/aws/aws-sdk-go-v2/service/directoryservice v1.38.19
	github.com/aws/aws-sdk-go-v2/service/docdb v1.48.16
	github.com/aws/aws-sdk-go-v2/service/docdbelastic v1.21.2
	github.com/aws/aws-sdk-go-v2/service/drs v1.39.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.42.1
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.300.0
	github.com/aws/aws-sdk-go-v2/service/ecr v1.36.3
	github.com/aws/aws-sdk-go-v2/service/ecs v1.36.0
	github.com/aws/aws-sdk-go-v2/service/efs v1.41.17
	github.com/aws/aws-sdk-go-v2/service/eks v1.83.0
	github.com/aws/aws-sdk-go-v2/service/elasticache v1.52.2
	github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk v1.34.5
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing v1.33.27
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 v1.43.13
	github.com/aws/aws-sdk-go-v2/service/emr v1.59.3
	github.com/aws/aws-sdk-go-v2/service/emrserverless v1.41.1
	github.com/aws/aws-sdk-go-v2/service/eventbridge v1.45.25
	github.com/aws/aws-sdk-go-v2/service/firehose v1.42.17
	github.com/aws/aws-sdk-go-v2/service/fis v1.38.2
	github.com/aws/aws-sdk-go-v2/service/fms v1.45.2
	github.com/aws/aws-sdk-go-v2/service/fsx v1.66.1
	github.com/aws/aws-sdk-go-v2/service/globalaccelerator v1.36.1
	github.com/aws/aws-sdk-go-v2/service/glue v1.142.0
	github.com/aws/aws-sdk-go-v2/service/grafana v1.36.0
	github.com/aws/aws-sdk-go-v2/service/guardduty v1.78.0
	github.com/aws/aws-sdk-go-v2/service/iam v1.36.3
	github.com/aws/aws-sdk-go-v2/service/identitystore v1.37.1
	github.com/aws/aws-sdk-go-v2/service/imagebuilder v1.55.2
	github.com/aws/aws-sdk-go-v2/service/inspector2 v1.48.1
	github.com/aws/aws-sdk-go-v2/service/kafka v1.52.0
	github.com/aws/aws-sdk-go-v2/service/keyspaces v1.26.1
	github.com/aws/aws-sdk-go-v2/service/kinesis v1.43.8
	github.com/aws/aws-sdk-go-v2/service/kinesisanalyticsv2 v1.38.1
	github.com/aws/aws-sdk-go-v2/service/kinesisvideo v1.33.11
	github.com/aws/aws-sdk-go-v2/service/kms v1.52.0
	github.com/aws/aws-sdk-go-v2/service/lakeformation v1.47.10
	github.com/aws/aws-sdk-go-v2/service/lambda v1.90.1
	github.com/aws/aws-sdk-go-v2/service/licensemanager v1.37.14
	github.com/aws/aws-sdk-go-v2/service/lightsail v1.53.2
	github.com/aws/aws-sdk-go-v2/service/location v1.52.2
	github.com/aws/aws-sdk-go-v2/service/macie2 v1.51.3
	github.com/aws/aws-sdk-go-v2/service/memorydb v1.34.1
	github.com/aws/aws-sdk-go-v2/service/mgn v1.44.2
	github.com/aws/aws-sdk-go-v2/service/mq v1.34.23
	github.com/aws/aws-sdk-go-v2/service/mwaa v1.40.2
	github.com/aws/aws-sdk-go-v2/service/neptune v1.44.6
	github.com/aws/aws-sdk-go-v2/service/neptunegraph v1.22.0
	github.com/aws/aws-sdk-go-v2/service/networkfirewall v1.61.1
	github.com/aws/aws-sdk-go-v2/service/networkmanager v1.42.2
	github.com/aws/aws-sdk-go-v2/service/opensearch v1.70.1
	github.com/aws/aws-sdk-go-v2/service/opensearchserverless v1.31.1
	github.com/aws/aws-sdk-go-v2/service/organizations v1.51.3
	github.com/aws/aws-sdk-go-v2/service/outposts v1.60.2
	github.com/aws/aws-sdk-go-v2/service/pinpoint v1.39.25
	github.com/aws/aws-sdk-go-v2/service/proton v1.40.0
	github.com/aws/aws-sdk-go-v2/service/quicksight v1.112.0
	github.com/aws/aws-sdk-go-v2/service/ram v1.36.6
	github.com/aws/aws-sdk-go-v2/service/rds v1.118.2
	github.com/aws/aws-sdk-go-v2/service/redshift v1.62.8
	github.com/aws/aws-sdk-go-v2/service/redshiftserverless v1.35.0
	github.com/aws/aws-sdk-go-v2/service/resiliencehub v1.36.2
	github.com/aws/aws-sdk-go-v2/service/resourcegroups v1.33.27
	github.com/aws/aws-sdk-go-v2/service/rolesanywhere v1.23.2
	github.com/aws/aws-sdk-go-v2/service/route53 v1.36.0
	github.com/aws/aws-sdk-go-v2/service/route53recoverycontrolconfig v1.33.1
	github.com/aws/aws-sdk-go-v2/service/route53resolver v1.36.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.97.3
	github.com/aws/aws-sdk-go-v2/service/sagemaker v1.249.0
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.41.7
	github.com/aws/aws-sdk-go-v2/service/securityhub v1.71.0
	github.com/aws/aws-sdk-go-v2/service/securitylake v1.25.17
	github.com/aws/aws-sdk-go-v2/service/servicecatalog v1.39.14
	github.com/aws/aws-sdk-go-v2/service/servicecatalogappregistry v1.36.2
	github.com/aws/aws-sdk-go-v2/service/servicediscovery v1.40.1
	github.com/aws/aws-sdk-go-v2/service/servicequotas v1.35.2
	github.com/aws/aws-sdk-go-v2/service/sesv2 v1.62.0
	github.com/aws/aws-sdk-go-v2/service/sfn v1.41.0
	github.com/aws/aws-sdk-go-v2/service/shield v1.34.25
	github.com/aws/aws-sdk-go-v2/service/signer v1.33.2
	github.com/aws/aws-sdk-go-v2/service/sns v1.39.17
	github.com/aws/aws-sdk-go-v2/service/sqs v1.42.27
	github.com/aws/aws-sdk-go-v2/service/ssm v1.68.6
	github.com/aws/aws-sdk-go-v2/service/ssoadmin v1.39.1
	github.com/aws/aws-sdk-go-v2/service/storagegateway v1.43.17
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.20
	github.com/aws/aws-sdk-go-v2/service/synthetics v1.43.0
	github.com/aws/aws-sdk-go-v2/service/timestreamwrite v1.35.25
	github.com/aws/aws-sdk-go-v2/service/transfer v1.72.2
	github.com/aws/aws-sdk-go-v2/service/verifiedpermissions v1.34.1
	github.com/aws/aws-sdk-go-v2/service/vpclattice v1.21.2
	github.com/aws/aws-sdk-go-v2/service/wafv2 v1.71.6
	github.com/aws/aws-sdk-go-v2/service/workspaces v1.68.3
	github.com/aws/aws-sdk-go-v2/service/xray v1.36.25
	github.com/aws/smithy-go v1.26.0
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/coreos/go-oidc/v3 v3.19.0
	github.com/crewjam/saml v0.5.1
	github.com/dekobon/tree-sitter-groovy v0.2.2
	github.com/eshu-hq/eshu/sdk/go/collector v0.0.0
	github.com/eshu-hq/eshu/sdk/go/factschema v0.0.0
	github.com/fergusstrange/embedded-postgres v1.34.0
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/hashicorp/hcl/v2 v2.24.0
	github.com/indigo-net/Brf.it v0.21.0
	github.com/jackc/pgx/v5 v5.9.2
	github.com/lib/pq v1.10.9
	github.com/mattn/go-isatty v0.0.21
	github.com/neo4j/neo4j-go-driver/v5 v5.28.4
	github.com/orneryd/nornicdb v1.0.45
	github.com/pelletier/go-toml/v2 v2.3.1
	github.com/prometheus/client_golang v1.20.5
	github.com/scip-code/scip/bindings/go/scip v0.7.0
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.9
	github.com/stretchr/testify v1.11.1
	github.com/tree-sitter-grammars/tree-sitter-kotlin v1.1.0
	github.com/tree-sitter/go-tree-sitter v0.25.0
	github.com/tree-sitter/tree-sitter-c v0.24.1
	github.com/tree-sitter/tree-sitter-c-sharp v0.23.1
	github.com/tree-sitter/tree-sitter-cpp v0.23.4
	github.com/tree-sitter/tree-sitter-elixir v0.0.0-00010101000000-000000000000
	github.com/tree-sitter/tree-sitter-go v0.25.0
	github.com/tree-sitter/tree-sitter-haskell v0.23.1
	github.com/tree-sitter/tree-sitter-java v0.23.5
	github.com/tree-sitter/tree-sitter-javascript v0.25.0
	github.com/tree-sitter/tree-sitter-php v0.24.2
	github.com/tree-sitter/tree-sitter-python v0.25.0
	github.com/tree-sitter/tree-sitter-ruby v0.23.1
	github.com/tree-sitter/tree-sitter-rust v0.24.2
	github.com/tree-sitter/tree-sitter-scala v0.25.0
	github.com/tree-sitter/tree-sitter-typescript v0.23.2
	github.com/zclconf/go-cty v1.18.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.68.0
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.34.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.34.0
	go.opentelemetry.io/otel/exporters/prometheus v0.56.0
	go.opentelemetry.io/otel/metric v1.43.0
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/sdk/metric v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
	golang.org/x/crypto v0.53.0
	golang.org/x/mod v0.37.0
	golang.org/x/net v0.56.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/sys v0.46.0
	golang.org/x/time v0.15.0
	google.golang.org/api v0.278.0
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.34.1
	k8s.io/apimachinery v0.34.1
	k8s.io/client-go v0.34.1
	sigs.k8s.io/yaml v1.6.0
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.7.0 // indirect
	cloud.google.com/go/kms v1.31.0 // indirect
	cloud.google.com/go/longrunning v0.9.0 // indirect
	github.com/99designs/gqlgen v0.17.90 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.10.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/keyvault/azkeys v0.10.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/keyvault/internal v0.7.1 // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.29 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.22 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/to v0.4.0 // indirect
	github.com/Azure/go-autorest/logger v0.2.1 // indirect
	github.com/Azure/go-autorest/tracing v0.6.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.3.3 // indirect
	github.com/agext/levenshtein v1.2.1 // indirect
	github.com/agnivade/levenshtein v1.2.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/aws/aws-sdk-go v1.55.5 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.10 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.25 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.25 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.26 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.12.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.25.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.30.1 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/beevik/etree v1.6.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/harmonica v0.2.0 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/chewxy/math32 v1.11.1 // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgraph-io/badger/v4 v4.9.1 // indirect
	github.com/dgraph-io/ristretto/v2 v2.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/google/flatbuffers v25.2.10+incompatible // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.15 // indirect
	github.com/googleapis/gax-go/v2 v2.22.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.25.1 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-kms-wrapping/v2 v2.0.22 // indirect
	github.com/hashicorp/go-kms-wrapping/wrappers/awskms/v2 v2.0.11 // indirect
	github.com/hashicorp/go-kms-wrapping/wrappers/azurekeyvault/v2 v2.0.14 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-secure-stdlib/awsutil v0.1.6 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.1.9 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.6 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/hybridgroup/yzma v1.13.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jupiterrider/ffi v0.6.0 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattermost/xml-roundtrip-validator v0.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.61.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/qdrant/go-client v1.17.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/russellhaering/goxmldsig v1.6.0 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sosodev/duration v1.4.0 // indirect
	github.com/sourcegraph/beaut v0.0.0-20240611013027-627e4c25335a // indirect
	github.com/vektah/gqlparser/v2 v2.5.33 // indirect
	github.com/viterin/partial v1.1.0 // indirect
	github.com/viterin/vek v0.4.3 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.34.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/term v0.44.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	golang.org/x/tools v0.47.0 // indirect
	google.golang.org/genproto v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260427160629-7cedc36a6bc4 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250710124328-f3f2b991d03b // indirect
	k8s.io/utils v0.0.0-20250604170112-4c0f3b243397 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect
)

replace github.com/eshu-hq/eshu/sdk/go/collector => ../sdk/go/collector

replace github.com/eshu-hq/eshu/sdk/go/factschema => ../sdk/go/factschema

replace github.com/tree-sitter/tree-sitter-elixir => github.com/elixir-lang/tree-sitter-elixir v0.3.5
