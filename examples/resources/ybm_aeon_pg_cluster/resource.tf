# A shared PostgreSQL cluster in the account's storage pool
resource "ybm_aeon_pg_cluster" "example" {
  cluster_name = "orders"
  region       = "us-east-1"

  # Optional
  pg_version = 18

  # The main branch: PostgreSQL settings for every endpoint on it, and extensions,
  # which can be added in place but not removed
  branch_pg_settings = {
    max_connections = "200"
  }
  branch_extensions = ["vector"]
}

# The read-write endpoint on main, which sets the cluster's password
resource "ybm_aeon_pg_endpoint" "primary" {
  cluster_name  = ybm_aeon_pg_cluster.example.cluster_name
  cluster_id    = ybm_aeon_pg_cluster.example.cluster_id
  endpoint_name = "primary"
  role          = "read_write"

  credentials = {
    password = var.pg_password # 8 to 128 printable ASCII characters
  }

  # Optional
  cpu               = "2"
  memory            = "16Gi" # at least 8Gi per vCPU
  connectivity_mode = "nlb_internal"
  allowed_cidrs     = ["10.0.0.0/16"]
}

output "connection" {
  value = "postgresql://${ybm_aeon_pg_endpoint.primary.username}@${ybm_aeon_pg_endpoint.primary.host}:${ybm_aeon_pg_endpoint.primary.port}/${ybm_aeon_pg_endpoint.primary.database}"
}
