module "service_edge_api_edge" {
  source      = "git::https://github.com/example/infra-modules-shared.git//modules/edge-service"
  app_repo    = "service-edge-api"
  config_path = "/configd/service-edge-api/runtime"
}
