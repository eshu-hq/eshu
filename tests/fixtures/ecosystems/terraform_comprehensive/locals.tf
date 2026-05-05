locals {
  common_tags = merge(var.tags, {
    Project     = "comprehensive"
    ManagedBy   = "terraform"
    Environment = var.environment
  })

  name_prefix = "comprehensive-${var.environment}"
  is_production = var.environment == "production"

  subnet_cidrs = [
    for i in range(var.vpc_config.subnet_count) :
    cidrsubnet(var.vpc_config.cidr_block, 8, i)
  ]
}
