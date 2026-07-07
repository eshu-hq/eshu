// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

const (
	// ResourceTypeAWSAccount identifies an AWS account relationship target.
	ResourceTypeAWSAccount = "aws_account"
	// ResourceTypeGeneric identifies a generic AWS resource fallback target.
	ResourceTypeGeneric = "aws_resource"

	// ResourceTypeEC2VPC identifies an EC2 VPC.
	ResourceTypeEC2VPC = "aws_ec2_vpc"
	// ResourceTypeEC2Subnet identifies an EC2 subnet.
	ResourceTypeEC2Subnet = "aws_ec2_subnet"
	// ResourceTypeEC2SecurityGroup identifies an EC2 security group.
	ResourceTypeEC2SecurityGroup = "aws_ec2_security_group"
	// ResourceTypeEC2SecurityGroupRule identifies an EC2 security group rule.
	ResourceTypeEC2SecurityGroupRule = "aws_ec2_security_group_rule"
	// ResourceTypeEC2NetworkInterface identifies an EC2 network interface.
	ResourceTypeEC2NetworkInterface = "aws_ec2_network_interface"
	// ResourceTypeEC2Volume identifies an EBS volume observed through EC2.
	ResourceTypeEC2Volume = "aws_ec2_volume"
	// ResourceTypeEC2Instance identifies an EC2 instance.
	ResourceTypeEC2Instance = "aws_ec2_instance"

	// ResourceTypeIAMRole identifies an IAM role.
	ResourceTypeIAMRole = "aws_iam_role"
	// ResourceTypeIAMUser identifies an IAM user.
	ResourceTypeIAMUser = "aws_iam_user"
	// ResourceTypeIAMGroup identifies an IAM group.
	ResourceTypeIAMGroup = "aws_iam_group"
	// ResourceTypeIAMPolicy identifies an IAM policy.
	ResourceTypeIAMPolicy = "aws_iam_policy"
	// ResourceTypeIAMInstanceProfile identifies an IAM instance profile.
	ResourceTypeIAMInstanceProfile = "aws_iam_instance_profile"
	// ResourceTypeIAMPrincipal identifies a principal from an IAM trust policy.
	ResourceTypeIAMPrincipal = "aws_iam_principal"

	// ResourceTypeS3Bucket identifies an S3 bucket.
	ResourceTypeS3Bucket = "aws_s3_bucket"
	// ResourceTypeKMSKey identifies a KMS key.
	ResourceTypeKMSKey = "aws_kms_key"
	// ResourceTypeSecretsManagerSecret identifies a Secrets Manager secret.
	ResourceTypeSecretsManagerSecret = "aws_secretsmanager_secret"
	// ResourceTypeSSMParameter identifies an SSM parameter.
	ResourceTypeSSMParameter = "aws_ssm_parameter"
	// ResourceTypeDynamoDBTable identifies a DynamoDB table.
	ResourceTypeDynamoDBTable = "aws_dynamodb_table"
	// ResourceTypeRDSDBInstance identifies an RDS DB instance.
	ResourceTypeRDSDBInstance = "aws_rds_db_instance"
	// ResourceTypeLambdaFunction identifies a Lambda function.
	ResourceTypeLambdaFunction = "aws_lambda_function"
)
