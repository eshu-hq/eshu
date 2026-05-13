package ecs

import (
	"context"
	"time"
)

// Client is the ECS read surface consumed by Scanner. Runtime adapters should
// translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListClusters(context.Context) ([]Cluster, error)
	ListServices(context.Context, Cluster) ([]Service, error)
	DescribeTaskDefinition(context.Context, string) (*TaskDefinition, error)
	ListTaskDefinitions(context.Context) ([]string, error)
	ListTasks(context.Context, Cluster) ([]Task, error)
}

// Cluster is the scanner-owned representation of an ECS cluster.
type Cluster struct {
	ARN                               string
	Name                              string
	Status                            string
	RunningTasksCount                 int32
	PendingTasksCount                 int32
	ActiveServicesCount               int32
	RegisteredContainerInstancesCount int32
	Tags                              map[string]string
}

// Service is the scanner-owned representation of an ECS service.
type Service struct {
	ARN               string
	Name              string
	ClusterARN        string
	Status            string
	LaunchType        string
	PlatformVersion   string
	TaskDefinitionARN string
	DesiredCount      int32
	RunningCount      int32
	PendingCount      int32
	LoadBalancers     []LoadBalancer
	Tags              map[string]string
}

// LoadBalancer is the scanner-owned ECS service load-balancer binding.
type LoadBalancer struct {
	TargetGroupARN   string
	LoadBalancerName string
	ContainerName    string
	ContainerPort    int32
}

// TaskDefinition is the scanner-owned representation of an ECS task definition.
type TaskDefinition struct {
	ARN                     string
	Family                  string
	Revision                int32
	Status                  string
	TaskRole                string
	ExecRole                string
	Network                 string
	CPU                     string
	Memory                  string
	RequiresCompatibilities []string
	CreatedAt               time.Time
	Containers              []Container
	Tags                    map[string]string
}

// Container is the scanner-owned representation of an ECS container
// definition.
type Container struct {
	Name        string
	Image       string
	Essential   bool
	Environment []EnvironmentVariable
	Secrets     []SecretReference
}

// EnvironmentVariable carries one ECS task-definition environment key/value
// pair. Values must be redacted before persistence.
type EnvironmentVariable struct {
	Name  string
	Value string
}

// SecretReference carries one ECS secret reference. ValueFrom is an ARN or
// provider-specific reference, not the secret value.
type SecretReference struct {
	Name      string
	ValueFrom string
}

// Task is the scanner-owned representation of an ECS task.
type Task struct {
	ARN               string
	ClusterARN        string
	TaskDefinitionARN string
	LastStatus        string
	DesiredStatus     string
	LaunchType        string
	Group             string
	StartedAt         time.Time
	Containers        []TaskContainer
}

// TaskContainer is the scanner-owned representation of a container on a
// described ECS task.
type TaskContainer struct {
	Name        string
	Image       string
	ImageDigest string
	RuntimeID   string
}
