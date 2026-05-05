module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.1.0"

  name = "comprehensive-vpc"
  cidr = var.vpc_config.cidr_block

  azs             = ["us-east-1a", "us-east-1b", "us-east-1c"]
  private_subnets = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
  public_subnets  = ["10.0.101.0/24", "10.0.102.0/24", "10.0.103.0/24"]

  enable_nat_gateway = var.vpc_config.enable_nat
}

module "s3_bucket" {
  source = "./modules/s3"

  bucket_name = "local-module-bucket"
  environment = var.environment
}

module "eks" {
  source = "git::https://github.com/example/terraform-aws-eks.git?ref=v1.0.0"

  cluster_name = "comprehensive-cluster"
  vpc_id       = module.vpc.vpc_id
  subnet_ids   = module.vpc.private_subnets
}
