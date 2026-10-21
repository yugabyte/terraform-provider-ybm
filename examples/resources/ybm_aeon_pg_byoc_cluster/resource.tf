resource "ybm_aeon_pg_byoc_cluster" "example" {
  environment_id = ybm_byoc_environment.example.environment_id
  storage_pool   = ybm_byoc_storage_pool.payments.name
  cluster_name   = "orders"

  # Optional
  pg_version = 18
  branch_pg_settings = {
    max_connections = "200"
  }
}

# The read-write endpoint on main, which sets the cluster's password
resource "ybm_aeon_pg_endpoint" "byoc_primary" {
  environment_id = ybm_aeon_pg_byoc_cluster.example.environment_id
  storage_pool   = ybm_aeon_pg_byoc_cluster.example.storage_pool
  cluster_name   = ybm_aeon_pg_byoc_cluster.example.cluster_name
  cluster_id     = ybm_aeon_pg_byoc_cluster.example.cluster_id
  endpoint_name  = "primary"
  role           = "read_write"

  credentials = {
    password = var.pg_password # 8 to 128 printable ASCII characters
  }

  cpu               = "4"
  connectivity_mode = "nlb_internal"
}
