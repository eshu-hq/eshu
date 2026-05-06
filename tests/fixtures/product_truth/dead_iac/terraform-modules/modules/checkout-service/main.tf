variable "service_name" {
  type = string
}

resource "aws_ecs_service" "this" {
  name            = var.service_name
  cluster         = "fixture-cluster"
  task_definition = "fixture-task"
  desired_count   = 1
}
