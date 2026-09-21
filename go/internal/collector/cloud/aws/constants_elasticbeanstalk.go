// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceElasticBeanstalk identifies the regional AWS Elastic Beanstalk
	// service scan slice.
	ServiceElasticBeanstalk = "elasticbeanstalk"
)

const (
	// ResourceTypeElasticBeanstalkApplication identifies an Elastic Beanstalk
	// application.
	ResourceTypeElasticBeanstalkApplication = "aws_elasticbeanstalk_application"
	// ResourceTypeElasticBeanstalkEnvironment identifies an Elastic Beanstalk
	// environment.
	ResourceTypeElasticBeanstalkEnvironment = "aws_elasticbeanstalk_environment"
	// ResourceTypeElasticBeanstalkApplicationVersion identifies an Elastic
	// Beanstalk application version.
	ResourceTypeElasticBeanstalkApplicationVersion = "aws_elasticbeanstalk_application_version"
)

const (
	// RelationshipElasticBeanstalkEnvironmentBelongsToApplication records the
	// application an environment belongs to.
	RelationshipElasticBeanstalkEnvironmentBelongsToApplication = "elasticbeanstalk_environment_belongs_to_application"
	// RelationshipElasticBeanstalkEnvironmentUsesVPC records the VPC an
	// environment is placed in, derived from the aws:ec2:vpc/VPCId option
	// setting. The target is the EC2-owned aws_ec2_vpc identity.
	RelationshipElasticBeanstalkEnvironmentUsesVPC = "elasticbeanstalk_environment_uses_vpc"
	// RelationshipElasticBeanstalkEnvironmentUsesInstanceProfile records the
	// EC2 instance profile an environment's instances assume, derived from the
	// aws:autoscaling:launchconfiguration/IamInstanceProfile option setting.
	RelationshipElasticBeanstalkEnvironmentUsesInstanceProfile = "elasticbeanstalk_environment_uses_instance_profile"
	// RelationshipElasticBeanstalkEnvironmentUsesServiceRole records the
	// Elastic Beanstalk service role an environment uses, derived from the
	// aws:elasticbeanstalk:environment/ServiceRole option setting.
	RelationshipElasticBeanstalkEnvironmentUsesServiceRole = "elasticbeanstalk_environment_uses_service_role"
	// RelationshipElasticBeanstalkEnvironmentUsesLoadBalancer records a load
	// balancer an environment routes through, reported by
	// DescribeEnvironmentResources. The target is the ELBv2-owned
	// aws_elbv2_load_balancer identity.
	RelationshipElasticBeanstalkEnvironmentUsesLoadBalancer = "elasticbeanstalk_environment_uses_load_balancer"
	// RelationshipElasticBeanstalkEnvironmentUsesAutoScalingGroup records an
	// Auto Scaling group an environment runs on, reported by
	// DescribeEnvironmentResources.
	RelationshipElasticBeanstalkEnvironmentUsesAutoScalingGroup = "elasticbeanstalk_environment_uses_auto_scaling_group"
	// RelationshipElasticBeanstalkEnvironmentUsesLaunchTemplate records an EC2
	// launch template an environment uses, reported by
	// DescribeEnvironmentResources.
	RelationshipElasticBeanstalkEnvironmentUsesLaunchTemplate = "elasticbeanstalk_environment_uses_launch_template"
	// RelationshipElasticBeanstalkEnvironmentRunsVersion records the
	// application version currently deployed in an environment.
	RelationshipElasticBeanstalkEnvironmentRunsVersion = "elasticbeanstalk_environment_runs_version"
)
