resource "aws_rds_cluster" "shared" {
  cluster_identifier = "shared-platform-db"
  engine             = "aurora-postgresql"
}
