resource "aws_ecs_cluster" "legacy_edge" {
  name = "legacy-edge"
}

module "service_edge_api" {
  source = "git::https://github.com/example/ecs-application/aws"
  name = "service-edge-api"
  app_repo = "service-edge-api"
  cluster_name = aws_ecs_cluster.legacy_edge.name
  config_path = "/api/service-edge-api/runtime"
}

module "shared_edge_service" {
  source = "git::https://github.com/example/infra-modules-shared.git//modules/edge-service"
  app_repo = "service-edge-api"
}
