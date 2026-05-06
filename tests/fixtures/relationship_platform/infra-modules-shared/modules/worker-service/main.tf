variable "app_repo" {
  type = string
  default = "service-worker-jobs"
}

output "app_repo" {
  value = var.app_repo
}
