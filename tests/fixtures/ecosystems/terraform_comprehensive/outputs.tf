output "instance_id" {
  description = "ID of the EC2 instance"
  value       = aws_instance.web.id
}

output "bucket_arn" {
  description = "ARN of the S3 bucket"
  value       = aws_s3_bucket.data.arn
}

output "role_arn" {
  description = "ARN of the IAM role"
  value       = aws_iam_role.irsa_role.arn
  sensitive   = false
}

output "account_id" {
  description = "AWS account ID"
  value       = data.aws_caller_identity.current.account_id
}
