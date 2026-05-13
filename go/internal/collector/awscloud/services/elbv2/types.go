package elbv2

import (
	"context"
	"time"
)

// Client is the ELBv2 read surface consumed by Scanner. Runtime adapters should
// translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListLoadBalancers(context.Context) ([]LoadBalancer, error)
	ListListeners(context.Context, LoadBalancer) ([]Listener, error)
	ListRules(context.Context, Listener) ([]Rule, error)
	ListTargetGroups(context.Context) ([]TargetGroup, error)
}

// LoadBalancer is the scanner-owned representation of an ELBv2 load balancer.
type LoadBalancer struct {
	ARN                   string
	Name                  string
	DNSName               string
	CanonicalHostedZoneID string
	Scheme                string
	Type                  string
	State                 string
	VPCID                 string
	IPAddressType         string
	CreatedAt             time.Time
	AvailabilityZones     []AvailabilityZone
	SecurityGroups        []string
	Tags                  map[string]string
}

// AvailabilityZone describes one load balancer subnet placement.
type AvailabilityZone struct {
	Name     string
	SubnetID string
}

// Listener is the scanner-owned representation of an ELBv2 listener.
type Listener struct {
	ARN             string
	LoadBalancerARN string
	Protocol        string
	Port            int32
	SSLPolicy       string
	Certificates    []string
	ALPNPolicy      []string
	DefaultActions  []Action
	Tags            map[string]string
}

// Rule is the scanner-owned representation of an ELBv2 listener rule.
type Rule struct {
	ARN         string
	ListenerARN string
	Priority    string
	IsDefault   bool
	Conditions  []Condition
	Actions     []Action
	Tags        map[string]string
}

// TargetGroup is the scanner-owned representation of an ELBv2 target group.
type TargetGroup struct {
	ARN              string
	Name             string
	Protocol         string
	ProtocolVersion  string
	Port             int32
	TargetType       string
	VPCID            string
	IPAddressType    string
	LoadBalancerARNs []string
	HealthCheck      HealthCheck
	Tags             map[string]string
}

// HealthCheck captures target group health-check configuration, not live target
// health status.
type HealthCheck struct {
	Enabled            bool
	Protocol           string
	Path               string
	Port               string
	IntervalSeconds    int32
	TimeoutSeconds     int32
	HealthyThreshold   int32
	UnhealthyThreshold int32
	Matcher            string
}

// Action is a typed ELBv2 listener or rule action with routing evidence.
type Action struct {
	Type                string
	Order               int32
	TargetGroupARN      string
	ForwardTargetGroups []WeightedTargetGroup
	Redirect            *RedirectAction
	FixedResponse       *FixedResponseAction
}

// WeightedTargetGroup is one target group in a forward action.
type WeightedTargetGroup struct {
	ARN    string
	Weight int32
}

// RedirectAction captures the non-secret redirect target.
type RedirectAction struct {
	StatusCode string
	Host       string
	Path       string
	Port       string
	Protocol   string
	Query      string
}

// FixedResponseAction captures a fixed-response action.
type FixedResponseAction struct {
	StatusCode  string
	ContentType string
	MessageBody string
}

// Condition is a typed ELBv2 listener-rule condition.
type Condition struct {
	Field              string
	Values             []string
	HostHeaderValues   []string
	HTTPHeaderName     string
	HTTPHeaderValues   []string
	HTTPRequestMethods []string
	PathPatternValues  []string
	QueryStrings       []QueryStringCondition
	SourceIPValues     []string
}

// QueryStringCondition is one typed query-string condition.
type QueryStringCondition struct {
	Key   string
	Value string
}
