variable "cache_name" {
  type    = string
  default = "orphan-cache"
}

resource "aws_elasticache_cluster" "this" {
  cluster_id           = var.cache_name
  engine               = "redis"
  node_type            = "cache.t4g.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis7"
}
