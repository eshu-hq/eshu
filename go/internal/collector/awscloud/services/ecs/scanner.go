package ecs

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const containerImageTargetType = "container_image"

// Scanner emits AWS ECS cluster, service, task-definition, task, and
// relationship facts for one claimed account and region.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

// Scan observes ECS resources through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ecs scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("ecs scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceECS
	case awscloud.ServiceECS:
	default:
		return nil, fmt.Errorf("ecs scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ECS clusters: %w", err)
	}
	var envelopes []facts.Envelope
	seenTaskDefinitions := map[string]struct{}{}
	for _, cluster := range clusters {
		resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		services, err := s.Client.ListServices(ctx, cluster)
		if err != nil {
			return nil, fmt.Errorf("list ECS services for cluster %q: %w", cluster.Name, err)
		}
		for _, service := range services {
			serviceEnvelopes, err := s.serviceEnvelopes(ctx, boundary, service, seenTaskDefinitions)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, serviceEnvelopes...)
		}

		tasks, err := s.Client.ListTasks(ctx, cluster)
		if err != nil {
			return nil, fmt.Errorf("list ECS tasks for cluster %q: %w", cluster.Name, err)
		}
		for _, task := range tasks {
			taskEnvelopes, err := taskEnvelopes(boundary, task)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, taskEnvelopes...)
		}
	}
	taskDefinitionARNs, err := s.Client.ListTaskDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ECS task definitions: %w", err)
	}
	for _, taskDefinitionARN := range taskDefinitionARNs {
		taskDefinitionEnvelopes, err := s.taskDefinitionEnvelopes(ctx, boundary, taskDefinitionARN, seenTaskDefinitions)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, taskDefinitionEnvelopes...)
	}
	return envelopes, nil
}

func (s Scanner) serviceEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	service Service,
	seenTaskDefinitions map[string]struct{},
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(serviceObservation(boundary, service))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if strings.TrimSpace(service.TaskDefinitionARN) != "" {
		relationship, err := awscloud.NewRelationshipEnvelope(serviceTaskDefinitionRelationship(boundary, service))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
		taskDefinitionEnvelopes, err := s.taskDefinitionEnvelopes(ctx, boundary, service.TaskDefinitionARN, seenTaskDefinitions)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, taskDefinitionEnvelopes...)
	}
	for _, loadBalancer := range service.LoadBalancers {
		relationship, err := awscloud.NewRelationshipEnvelope(serviceLoadBalancerRelationship(boundary, service, loadBalancer))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func (s Scanner) taskDefinitionEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	taskDefinitionARN string,
	seen map[string]struct{},
) ([]facts.Envelope, error) {
	taskDefinitionARN = strings.TrimSpace(taskDefinitionARN)
	if taskDefinitionARN == "" {
		return nil, nil
	}
	if _, ok := seen[taskDefinitionARN]; ok {
		return nil, nil
	}
	seen[taskDefinitionARN] = struct{}{}
	taskDefinition, err := s.Client.DescribeTaskDefinition(ctx, taskDefinitionARN)
	if err != nil {
		return nil, fmt.Errorf("describe ECS task definition %q: %w", taskDefinitionARN, err)
	}
	if taskDefinition == nil {
		return nil, nil
	}
	resource, err := awscloud.NewResourceEnvelope(s.taskDefinitionObservation(boundary, *taskDefinition))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, container := range taskDefinition.Containers {
		if strings.TrimSpace(container.Image) == "" {
			continue
		}
		relationship, err := awscloud.NewRelationshipEnvelope(
			taskDefinitionImageRelationship(boundary, *taskDefinition, container),
		)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster Cluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          clusterARN,
		ResourceID:   clusterARN,
		ResourceType: awscloud.ResourceTypeECSCluster,
		Name:         cluster.Name,
		State:        cluster.Status,
		Tags:         cluster.Tags,
		Attributes: map[string]any{
			"active_services_count":                cluster.ActiveServicesCount,
			"pending_tasks_count":                  cluster.PendingTasksCount,
			"registered_container_instances_count": cluster.RegisteredContainerInstancesCount,
			"running_tasks_count":                  cluster.RunningTasksCount,
		},
		CorrelationAnchors: []string{clusterARN, strings.TrimSpace(cluster.Name)},
		SourceRecordID:     clusterARN,
	}
}

func serviceObservation(boundary awscloud.Boundary, service Service) awscloud.ResourceObservation {
	serviceARN := strings.TrimSpace(service.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          serviceARN,
		ResourceID:   serviceARN,
		ResourceType: awscloud.ResourceTypeECSService,
		Name:         service.Name,
		State:        service.Status,
		Tags:         service.Tags,
		Attributes: map[string]any{
			"cluster_arn":         strings.TrimSpace(service.ClusterARN),
			"desired_count":       service.DesiredCount,
			"launch_type":         strings.TrimSpace(service.LaunchType),
			"load_balancers":      loadBalancerMaps(service.LoadBalancers),
			"pending_count":       service.PendingCount,
			"platform_version":    strings.TrimSpace(service.PlatformVersion),
			"running_count":       service.RunningCount,
			"task_definition_arn": strings.TrimSpace(service.TaskDefinitionARN),
		},
		CorrelationAnchors: []string{serviceARN, strings.TrimSpace(service.Name), strings.TrimSpace(service.TaskDefinitionARN)},
		SourceRecordID:     serviceARN,
	}
}

func (s Scanner) taskDefinitionObservation(
	boundary awscloud.Boundary,
	taskDefinition TaskDefinition,
) awscloud.ResourceObservation {
	taskDefinitionARN := strings.TrimSpace(taskDefinition.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          taskDefinitionARN,
		ResourceID:   firstNonEmpty(taskDefinitionARN, taskDefinition.Family+":"+strconv.Itoa(int(taskDefinition.Revision))),
		ResourceType: awscloud.ResourceTypeECSTaskDefinition,
		Name:         strings.TrimSpace(taskDefinition.Family),
		State:        taskDefinition.Status,
		Attributes: map[string]any{
			"containers":               s.containerMaps(taskDefinition),
			"cpu":                      strings.TrimSpace(taskDefinition.CPU),
			"execution_role_arn":       strings.TrimSpace(taskDefinition.ExecRole),
			"memory":                   strings.TrimSpace(taskDefinition.Memory),
			"network_mode":             strings.TrimSpace(taskDefinition.Network),
			"registered_at":            timeOrNil(taskDefinition.CreatedAt),
			"requires_compatibilities": cloneStrings(taskDefinition.RequiresCompatibilities),
			"revision":                 taskDefinition.Revision,
			"task_role_arn":            strings.TrimSpace(taskDefinition.TaskRole),
		},
		CorrelationAnchors: []string{
			taskDefinitionARN,
			strings.TrimSpace(taskDefinition.Family),
			strings.TrimSpace(taskDefinition.Family) + ":" + strconv.Itoa(int(taskDefinition.Revision)),
		},
		SourceRecordID: taskDefinitionARN,
	}
}

func taskObservation(boundary awscloud.Boundary, task Task) awscloud.ResourceObservation {
	taskARN := strings.TrimSpace(task.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          taskARN,
		ResourceID:   taskARN,
		ResourceType: awscloud.ResourceTypeECSTask,
		Name:         taskARN,
		State:        task.LastStatus,
		Attributes: map[string]any{
			"cluster_arn":         strings.TrimSpace(task.ClusterARN),
			"containers":          taskContainerMaps(task.Containers),
			"desired_status":      strings.TrimSpace(task.DesiredStatus),
			"group":               strings.TrimSpace(task.Group),
			"launch_type":         strings.TrimSpace(task.LaunchType),
			"started_at":          timeOrNil(task.StartedAt),
			"task_definition_arn": strings.TrimSpace(task.TaskDefinitionARN),
		},
		CorrelationAnchors: []string{taskARN, strings.TrimSpace(task.TaskDefinitionARN)},
		SourceRecordID:     taskARN,
	}
}

func taskEnvelopes(boundary awscloud.Boundary, task Task) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(taskObservation(boundary, task))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{resource}, nil
}

func serviceTaskDefinitionRelationship(boundary awscloud.Boundary, service Service) awscloud.RelationshipObservation {
	serviceARN := strings.TrimSpace(service.ARN)
	taskDefinitionARN := strings.TrimSpace(service.TaskDefinitionARN)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipECSServiceUsesTaskDefinition,
		SourceResourceID: serviceARN,
		SourceARN:        serviceARN,
		TargetResourceID: taskDefinitionARN,
		TargetARN:        taskDefinitionARN,
		TargetType:       awscloud.ResourceTypeECSTaskDefinition,
		SourceRecordID:   serviceARN + "#task-definition#" + taskDefinitionARN,
	}
}

func taskDefinitionImageRelationship(
	boundary awscloud.Boundary,
	taskDefinition TaskDefinition,
	container Container,
) awscloud.RelationshipObservation {
	taskDefinitionARN := strings.TrimSpace(taskDefinition.ARN)
	image := strings.TrimSpace(container.Image)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipECSTaskDefinitionUsesImage,
		SourceResourceID: taskDefinitionARN,
		SourceARN:        taskDefinitionARN,
		TargetResourceID: image,
		TargetType:       containerImageTargetType,
		Attributes: map[string]any{
			"container_name": strings.TrimSpace(container.Name),
		},
		SourceRecordID: taskDefinitionARN + "#container-image#" + strings.TrimSpace(container.Name) + "#" + image,
	}
}

func serviceLoadBalancerRelationship(
	boundary awscloud.Boundary,
	service Service,
	loadBalancer LoadBalancer,
) awscloud.RelationshipObservation {
	serviceARN := strings.TrimSpace(service.ARN)
	targetID := firstNonEmpty(loadBalancer.TargetGroupARN, loadBalancer.LoadBalancerName)
	targetType := "aws_elb_load_balancer"
	if strings.TrimSpace(loadBalancer.TargetGroupARN) != "" {
		targetType = "aws_elbv2_target_group"
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipECSServiceTargetsLoadBalancer,
		SourceResourceID: serviceARN,
		SourceARN:        serviceARN,
		TargetResourceID: targetID,
		TargetARN:        strings.TrimSpace(loadBalancer.TargetGroupARN),
		TargetType:       targetType,
		Attributes: map[string]any{
			"container_name": strings.TrimSpace(loadBalancer.ContainerName),
			"container_port": loadBalancer.ContainerPort,
		},
		SourceRecordID: serviceARN + "#load-balancer#" + targetID,
	}
}

func (s Scanner) containerMaps(taskDefinition TaskDefinition) []map[string]any {
	if len(taskDefinition.Containers) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(taskDefinition.Containers))
	for _, container := range taskDefinition.Containers {
		output = append(output, map[string]any{
			"environment": environmentVariableMaps(taskDefinition, container, s.RedactionKey),
			"essential":   container.Essential,
			"image":       strings.TrimSpace(container.Image),
			"name":        strings.TrimSpace(container.Name),
			"secrets":     secretReferenceMaps(container.Secrets),
		})
	}
	return output
}

func environmentVariableMaps(
	taskDefinition TaskDefinition,
	container Container,
	key redact.Key,
) []map[string]any {
	if len(container.Environment) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(container.Environment))
	for _, variable := range container.Environment {
		source := strings.TrimSpace(taskDefinition.ARN) + ".container." +
			strings.TrimSpace(container.Name) + ".environment." + strings.TrimSpace(variable.Name)
		output = append(output, map[string]any{
			"name": strings.TrimSpace(variable.Name),
			"value": redactionMap(redact.String(
				variable.Value,
				redact.ReasonKnownSensitiveKey,
				source,
				key,
			)),
		})
	}
	return output
}

func secretReferenceMaps(secrets []SecretReference) []map[string]string {
	if len(secrets) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(secrets))
	for _, secret := range secrets {
		output = append(output, map[string]string{
			"name":       strings.TrimSpace(secret.Name),
			"value_from": strings.TrimSpace(secret.ValueFrom),
		})
	}
	return output
}

func loadBalancerMaps(loadBalancers []LoadBalancer) []map[string]any {
	if len(loadBalancers) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(loadBalancers))
	for _, loadBalancer := range loadBalancers {
		output = append(output, map[string]any{
			"container_name":     strings.TrimSpace(loadBalancer.ContainerName),
			"container_port":     loadBalancer.ContainerPort,
			"load_balancer_name": strings.TrimSpace(loadBalancer.LoadBalancerName),
			"target_group_arn":   strings.TrimSpace(loadBalancer.TargetGroupARN),
		})
	}
	return output
}

func taskContainerMaps(containers []TaskContainer) []map[string]string {
	if len(containers) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(containers))
	for _, container := range containers {
		output = append(output, map[string]string{
			"image":        strings.TrimSpace(container.Image),
			"image_digest": strings.TrimSpace(container.ImageDigest),
			"name":         strings.TrimSpace(container.Name),
			"runtime_id":   strings.TrimSpace(container.RuntimeID),
		})
	}
	return output
}

func redactionMap(value redact.Value) map[string]any {
	return map[string]any{
		"marker": value.Marker,
		"reason": value.Reason,
		"source": value.Source,
	}
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
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

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
