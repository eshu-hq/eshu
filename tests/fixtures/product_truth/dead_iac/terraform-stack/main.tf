variable "dynamic_module_name" {
  type    = string
  default = "dynamic-target"
}

module "checkout_service" {
  source = "../terraform-modules/modules/checkout-service"

  service_name = "checkout-service"
}

module "dynamic_target" {
  source = "../terraform-modules/modules/${var.dynamic_module_name}"

  service_name = "dynamic-service"
}
