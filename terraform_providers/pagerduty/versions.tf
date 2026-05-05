terraform {
  required_providers {
    pagerduty = {
      source  = "pagerduty/pagerduty"
      version = "~> 3.0"
    }
  }
}

provider "pagerduty" {
  token = "placeholder"
}
