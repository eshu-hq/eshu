resource "aws_eks_cluster" "modern_edge" {
  name = "modern-edge"

  role_arn = "arn:aws:iam::123456789012:role/example"

  vpc_config {
    subnet_ids = ["subnet-12345", "subnet-67890"]
  }
}

module "service_edge_api_modern" {
  source = "git::https://github.com/example/infra-modules-shared.git//modules/edge-service"
  app_repo = "service-edge-api"
  config_path = "/configd/service-edge-api/runtime"
}

module "service_worker_jobs_modern" {
  source = "git::https://github.com/example/infra-modules-shared.git//modules/worker-service"
  app_repo = "service-worker-jobs"
  config_path = "/configd/service-worker-jobs/runtime"
}
