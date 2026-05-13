package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	awsecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ecsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecs"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	describeClustersLimit = 100
	describeServicesLimit = 10
	describeTasksLimit    = 100
)

type apiClient interface {
	DescribeClusters(context.Context, *awsecs.DescribeClustersInput, ...func(*awsecs.Options)) (*awsecs.DescribeClustersOutput, error)
	awsecs.DescribeServicesAPIClient
	DescribeTaskDefinition(context.Context, *awsecs.DescribeTaskDefinitionInput, ...func(*awsecs.Options)) (*awsecs.DescribeTaskDefinitionOutput, error)
	awsecs.DescribeTasksAPIClient
	awsecs.ListClustersAPIClient
	awsecs.ListServicesAPIClient
	awsecs.ListTaskDefinitionsAPIClient
	awsecs.ListTasksAPIClient
}

// Client adapts AWS SDK ECS pagination into scanner-owned ECS records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an ECS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsecs.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListClusters returns all ECS clusters visible to the configured AWS
// credentials.
func (c *Client) ListClusters(ctx context.Context) ([]ecsservice.Cluster, error) {
	paginator := awsecs.NewListClustersPaginator(c.client, &awsecs.ListClustersInput{})
	var arns []string
	for paginator.HasMorePages() {
		var page *awsecs.ListClustersOutput
		err := c.recordAPICall(ctx, "ListClusters", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.ClusterArns...)
	}
	var clusters []ecsservice.Cluster
	for _, chunk := range chunkStrings(arns, describeClustersLimit) {
		var output *awsecs.DescribeClustersOutput
		err := c.recordAPICall(ctx, "DescribeClusters", func(callCtx context.Context) error {
			var err error
			output, err = c.client.DescribeClusters(callCtx, &awsecs.DescribeClustersInput{
				Clusters: chunk,
				Include:  []awsecstypes.ClusterField{awsecstypes.ClusterFieldTags},
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, cluster := range output.Clusters {
			clusters = append(clusters, mapCluster(cluster))
		}
	}
	return clusters, nil
}

// ListServices returns all ECS services in one cluster.
func (c *Client) ListServices(ctx context.Context, cluster ecsservice.Cluster) ([]ecsservice.Service, error) {
	clusterARN := strings.TrimSpace(cluster.ARN)
	paginator := awsecs.NewListServicesPaginator(c.client, &awsecs.ListServicesInput{
		Cluster: aws.String(clusterARN),
	})
	var arns []string
	for paginator.HasMorePages() {
		var page *awsecs.ListServicesOutput
		err := c.recordAPICall(ctx, "ListServices", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.ServiceArns...)
	}
	var services []ecsservice.Service
	for _, chunk := range chunkStrings(arns, describeServicesLimit) {
		var output *awsecs.DescribeServicesOutput
		err := c.recordAPICall(ctx, "DescribeServices", func(callCtx context.Context) error {
			var err error
			output, err = c.client.DescribeServices(callCtx, &awsecs.DescribeServicesInput{
				Cluster:  aws.String(clusterARN),
				Services: chunk,
				Include:  []awsecstypes.ServiceField{awsecstypes.ServiceFieldTags},
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, service := range output.Services {
			services = append(services, mapService(service))
		}
	}
	return services, nil
}

// DescribeTaskDefinition returns one ECS task definition by ARN or family
// revision.
func (c *Client) DescribeTaskDefinition(
	ctx context.Context,
	arn string,
) (*ecsservice.TaskDefinition, error) {
	var output *awsecs.DescribeTaskDefinitionOutput
	err := c.recordAPICall(ctx, "DescribeTaskDefinition", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeTaskDefinition(callCtx, &awsecs.DescribeTaskDefinitionInput{
			TaskDefinition: aws.String(arn),
			Include:        []awsecstypes.TaskDefinitionField{awsecstypes.TaskDefinitionFieldTags},
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.TaskDefinition == nil {
		return nil, nil
	}
	value := mapTaskDefinition(*output.TaskDefinition)
	return &value, nil
}

// ListTaskDefinitions returns all ECS task-definition ARNs visible to the
// configured AWS credentials.
func (c *Client) ListTaskDefinitions(ctx context.Context) ([]string, error) {
	paginator := awsecs.NewListTaskDefinitionsPaginator(c.client, &awsecs.ListTaskDefinitionsInput{})
	var arns []string
	for paginator.HasMorePages() {
		var page *awsecs.ListTaskDefinitionsOutput
		err := c.recordAPICall(ctx, "ListTaskDefinitions", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.TaskDefinitionArns...)
	}
	return arns, nil
}

// ListTasks returns all ECS tasks in one cluster.
func (c *Client) ListTasks(ctx context.Context, cluster ecsservice.Cluster) ([]ecsservice.Task, error) {
	clusterARN := strings.TrimSpace(cluster.ARN)
	paginator := awsecs.NewListTasksPaginator(c.client, &awsecs.ListTasksInput{
		Cluster: aws.String(clusterARN),
	})
	var arns []string
	for paginator.HasMorePages() {
		var page *awsecs.ListTasksOutput
		err := c.recordAPICall(ctx, "ListTasks", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.TaskArns...)
	}
	var tasks []ecsservice.Task
	for _, chunk := range chunkStrings(arns, describeTasksLimit) {
		var output *awsecs.DescribeTasksOutput
		err := c.recordAPICall(ctx, "DescribeTasks", func(callCtx context.Context) error {
			var err error
			output, err = c.client.DescribeTasks(callCtx, &awsecs.DescribeTasksInput{
				Cluster: aws.String(clusterARN),
				Tasks:   chunk,
				Include: []awsecstypes.TaskField{awsecstypes.TaskFieldTags},
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, task := range output.Tasks {
			tasks = append(tasks, mapTask(task))
		}
	}
	return tasks, nil
}

func mapCluster(cluster awsecstypes.Cluster) ecsservice.Cluster {
	return ecsservice.Cluster{
		ARN:                               aws.ToString(cluster.ClusterArn),
		Name:                              aws.ToString(cluster.ClusterName),
		Status:                            aws.ToString(cluster.Status),
		RunningTasksCount:                 cluster.RunningTasksCount,
		PendingTasksCount:                 cluster.PendingTasksCount,
		ActiveServicesCount:               cluster.ActiveServicesCount,
		RegisteredContainerInstancesCount: cluster.RegisteredContainerInstancesCount,
		Tags:                              mapTags(cluster.Tags),
	}
}

func mapService(service awsecstypes.Service) ecsservice.Service {
	return ecsservice.Service{
		ARN:               aws.ToString(service.ServiceArn),
		Name:              aws.ToString(service.ServiceName),
		ClusterARN:        aws.ToString(service.ClusterArn),
		Status:            aws.ToString(service.Status),
		LaunchType:        string(service.LaunchType),
		PlatformVersion:   aws.ToString(service.PlatformVersion),
		TaskDefinitionARN: aws.ToString(service.TaskDefinition),
		DesiredCount:      service.DesiredCount,
		RunningCount:      service.RunningCount,
		PendingCount:      service.PendingCount,
		LoadBalancers:     mapLoadBalancers(service.LoadBalancers),
		Tags:              mapTags(service.Tags),
	}
}

func mapTaskDefinition(taskDefinition awsecstypes.TaskDefinition) ecsservice.TaskDefinition {
	return ecsservice.TaskDefinition{
		ARN:                     aws.ToString(taskDefinition.TaskDefinitionArn),
		Family:                  aws.ToString(taskDefinition.Family),
		Revision:                taskDefinition.Revision,
		Status:                  string(taskDefinition.Status),
		TaskRole:                aws.ToString(taskDefinition.TaskRoleArn),
		ExecRole:                aws.ToString(taskDefinition.ExecutionRoleArn),
		Network:                 string(taskDefinition.NetworkMode),
		CPU:                     aws.ToString(taskDefinition.Cpu),
		Memory:                  aws.ToString(taskDefinition.Memory),
		RequiresCompatibilities: mapCompatibilities(taskDefinition.RequiresCompatibilities),
		CreatedAt:               aws.ToTime(taskDefinition.RegisteredAt),
		Containers:              mapContainers(taskDefinition.ContainerDefinitions),
	}
}

func mapTask(task awsecstypes.Task) ecsservice.Task {
	return ecsservice.Task{
		ARN:               aws.ToString(task.TaskArn),
		ClusterARN:        aws.ToString(task.ClusterArn),
		TaskDefinitionARN: aws.ToString(task.TaskDefinitionArn),
		LastStatus:        aws.ToString(task.LastStatus),
		DesiredStatus:     aws.ToString(task.DesiredStatus),
		LaunchType:        string(task.LaunchType),
		Group:             aws.ToString(task.Group),
		StartedAt:         aws.ToTime(task.StartedAt),
		Containers:        mapTaskContainers(task.Containers),
	}
}

func mapLoadBalancers(loadBalancers []awsecstypes.LoadBalancer) []ecsservice.LoadBalancer {
	if len(loadBalancers) == 0 {
		return nil
	}
	output := make([]ecsservice.LoadBalancer, 0, len(loadBalancers))
	for _, loadBalancer := range loadBalancers {
		output = append(output, ecsservice.LoadBalancer{
			TargetGroupARN:   aws.ToString(loadBalancer.TargetGroupArn),
			LoadBalancerName: aws.ToString(loadBalancer.LoadBalancerName),
			ContainerName:    aws.ToString(loadBalancer.ContainerName),
			ContainerPort:    aws.ToInt32(loadBalancer.ContainerPort),
		})
	}
	return output
}

func mapContainers(containers []awsecstypes.ContainerDefinition) []ecsservice.Container {
	if len(containers) == 0 {
		return nil
	}
	output := make([]ecsservice.Container, 0, len(containers))
	for _, container := range containers {
		output = append(output, ecsservice.Container{
			Name:        aws.ToString(container.Name),
			Image:       aws.ToString(container.Image),
			Essential:   aws.ToBool(container.Essential),
			Environment: mapEnvironment(container.Environment),
			Secrets:     mapSecrets(container.Secrets),
		})
	}
	return output
}

func mapEnvironment(environment []awsecstypes.KeyValuePair) []ecsservice.EnvironmentVariable {
	if len(environment) == 0 {
		return nil
	}
	output := make([]ecsservice.EnvironmentVariable, 0, len(environment))
	for _, variable := range environment {
		output = append(output, ecsservice.EnvironmentVariable{
			Name:  aws.ToString(variable.Name),
			Value: aws.ToString(variable.Value),
		})
	}
	return output
}

func mapSecrets(secrets []awsecstypes.Secret) []ecsservice.SecretReference {
	if len(secrets) == 0 {
		return nil
	}
	output := make([]ecsservice.SecretReference, 0, len(secrets))
	for _, secret := range secrets {
		output = append(output, ecsservice.SecretReference{
			Name:      aws.ToString(secret.Name),
			ValueFrom: aws.ToString(secret.ValueFrom),
		})
	}
	return output
}

func mapTaskContainers(containers []awsecstypes.Container) []ecsservice.TaskContainer {
	if len(containers) == 0 {
		return nil
	}
	output := make([]ecsservice.TaskContainer, 0, len(containers))
	for _, container := range containers {
		output = append(output, ecsservice.TaskContainer{
			Name:        aws.ToString(container.Name),
			Image:       aws.ToString(container.Image),
			ImageDigest: aws.ToString(container.ImageDigest),
			RuntimeID:   aws.ToString(container.RuntimeId),
		})
	}
	return output
}

func mapCompatibilities(values []awsecstypes.Compatibility) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		output = append(output, string(value))
	}
	return output
}

func mapTags(tags []awsecstypes.Tag) map[string]string {
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

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if isThrottleError(err) {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

func chunkStrings(values []string, size int) [][]string {
	if len(values) == 0 || size <= 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

var _ ecsservice.Client = (*Client)(nil)

var _ apiClient = (*awsecs.Client)(nil)
