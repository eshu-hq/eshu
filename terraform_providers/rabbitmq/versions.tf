terraform {
  required_providers {
    rabbitmq = {
      source  = "cyrilgdn/rabbitmq"
      version = "~> 1.0"
    }
  }
}

provider "rabbitmq" {
  endpoint = "http://localhost:15672"
  username = "placeholder"
  password = "placeholder"
}
