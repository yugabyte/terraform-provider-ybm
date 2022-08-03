terraform {
  required_providers {
    ybm = {
      version = "0.1.0"
      source = "registry.terraform.io/yugabyte/ybm"
    }
  }
}

variable "auth_token" {
  type        = string
  description = "The authentication token."
  sensitive = true
}

provider "ybm" {
  host = "devcloud.yugabyte.com"
  use_secure_host = true
  auth_token = var.auth_token
}

