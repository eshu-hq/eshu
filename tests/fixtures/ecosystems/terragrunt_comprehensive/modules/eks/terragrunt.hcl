terraform {
  source = "tfr:///terraform-aws-modules/eks/aws?version=19.0.0"
}

include "root" {
  path = find_in_parent_folders()
}

dependency "vpc" {
  config_path = "../vpc"
}

inputs = {
  cluster_name = "comprehensive-cluster"
  vpc_id       = dependency.vpc.outputs.vpc_id
  subnet_ids   = dependency.vpc.outputs.private_subnets
}
