resource "aws_security_group" "dynamic_example" {
  name        = "${local.name_prefix}-sg"
  description = "Security group with dynamic blocks"
  vpc_id      = module.vpc.vpc_id

  dynamic "ingress" {
    for_each = var.allowed_cidrs
    content {
      from_port   = 443
      to_port     = 443
      protocol    = "tcp"
      cidr_blocks = [ingress.value]
    }
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = local.common_tags
}

resource "aws_iam_user" "users" {
  for_each = toset(["alice", "bob", "charlie"])
  name     = each.key
  tags     = local.common_tags
}

resource "aws_subnet" "counted" {
  count      = var.vpc_config.subnet_count
  vpc_id     = module.vpc.vpc_id
  cidr_block = local.subnet_cidrs[count.index]

  tags = merge(local.common_tags, {
    Name = "${local.name_prefix}-subnet-${count.index}"
  })
}
