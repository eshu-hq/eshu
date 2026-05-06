terraform {
  required_version = ">= 1.6.0"
}

locals {
  app_name = "service-gha"
  app_repo = "https://github.com/example/service-gha.git"
}

module "service_runtime" {
  source   = "git::https://github.com/example/service-gha.git//deploy/terraform"
  app_name = local.app_name
  app_repo = local.app_repo
}

resource "null_resource" "service_subject" {
  triggers = {
    app_name               = local.app_name
    app_repo               = local.app_repo
    config_path            = "/configd/service-gha/runtime"
    github_actions_subject = "repo:example/service-gha:ref:refs/heads/main"
  }
}
