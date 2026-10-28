# The sole workload in this repository declares its exact ECS service identity.
# Runtime task observations come from the separate AWS cassette.
resource "aws_ecs_service" "deployable_source" {
  name            = "deployable-source"
  cluster         = "arn:aws:ecs:us-east-1:123456789012:cluster/supply-chain-demo"
  task_definition = "arn:aws:ecs:us-east-1:123456789012:task-definition/supply-chain-demo:1"
  desired_count   = 1
  launch_type     = "FARGATE"
}
