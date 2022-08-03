terraform {
  required_providers {
    ybm = {
      version = "0.1.0"
      source = "registry.terraform.io/yugabyte/ybm"
    }
  }
}

provider "ybm" {
  host = "cloud.yugabyte.com"
  use_secure_host = false # True by default
  auth_token = "authentication-token"
}