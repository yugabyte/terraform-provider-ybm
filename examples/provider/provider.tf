variable "auth_token" {
  type        = string
  description = "The authentication token."
  sensitive   = true
}

provider "ybm" {
  host = "cloud.yugabyte.com"
  use_secure_host = false # True by default
  auth_token = var.auth_token
}
