terraform {
  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "~> 1.0"
    }
  }
}

provider "alicloud" {
  region     = "cn-hangzhou"
  access_key = "placeholder"
  secret_key = "placeholder"
}
