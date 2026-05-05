terraform {
  source = "git::https://github.com/example/infra-modules-shared.git//modules/worker-service"
}

inputs = {
  service_name = "service-worker-jobs"
}
