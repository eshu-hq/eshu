variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Deployment environment"
  type        = string

  validation {
    condition     = contains(["dev", "staging", "production"], var.environment)
    error_message = "Environment must be dev, staging, or production."
  }
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.micro"
}

variable "bucket_name" {
  description = "S3 bucket name"
  type        = string
}

variable "enable_monitoring" {
  description = "Enable CloudWatch monitoring"
  type        = bool
  default     = true
}

variable "tags" {
  description = "Common tags for all resources"
  type        = map(string)
  default     = {}
}

variable "allowed_cidrs" {
  description = "List of allowed CIDR blocks"
  type        = list(string)
  default     = ["10.0.0.0/8"]
}

variable "vpc_config" {
  description = "VPC configuration"
  type = object({
    cidr_block     = string
    subnet_count   = number
    enable_nat     = bool
  })
  default = {
    cidr_block   = "10.0.0.0/16"
    subnet_count = 3
    enable_nat   = true
  }
}
