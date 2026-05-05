terraform {
  source = "tfr:///terraform-aws-modules/vpc/aws?version=5.1.0"
}

include "root" {
  path = find_in_parent_folders()
}

inputs = {
  name = "comprehensive-vpc"
  cidr = "10.0.0.0/16"
  azs  = ["us-east-1a", "us-east-1b"]
}
