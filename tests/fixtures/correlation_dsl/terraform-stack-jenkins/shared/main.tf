terraform {
  required_version = ">= 1.6.0"
}

locals {
  app_name = "service-jenkins"
  app_repo = "https://github.com/example/service-jenkins.git"
}

module "service_runtime" {
  source   = "git::https://github.com/example/service-jenkins.git//deploy/terraform"
  app_name = local.app_name
  app_repo = local.app_repo
}

resource "null_resource" "service_subject" {
  triggers = {
    app_name            = local.app_name
    app_repo            = local.app_repo
    config_path         = "/configd/service-jenkins/runtime"
    github_actions_role = "repo:example/service-jenkins:environment:prod"
  }
}
