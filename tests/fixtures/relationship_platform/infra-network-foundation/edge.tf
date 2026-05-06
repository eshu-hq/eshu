resource "aws_lb" "legacy_edge" {
  name               = "legacy-edge"
  load_balancer_type = "application"
  internal           = false
  subnets            = ["subnet-12345", "subnet-67890"]
}

resource "aws_vpc" "platform" {
  cidr_block = "10.0.0.0/16"
}
