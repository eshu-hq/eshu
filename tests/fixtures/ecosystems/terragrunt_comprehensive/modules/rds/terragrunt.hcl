terraform {
  source = "tfr:///terraform-aws-modules/rds/aws?version=6.0.0"
}

include "root" {
  path = find_in_parent_folders()
}

dependency "vpc" {
  config_path = "../vpc"
}

dependency "eks" {
  config_path = "../eks"
}

inputs = {
  identifier = "comprehensive-db"
  engine     = "postgres"
  vpc_id     = dependency.vpc.outputs.vpc_id
}
