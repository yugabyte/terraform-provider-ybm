resource "ybm_byoc_cluster" "example" {
  environment_id = ybm_byoc_environment.example.environment_id
  storage_pool   = ybm_byoc_storage_pool.payments.name
  cluster_name   = "orders"

  credentials = {
    password = var.byoc_password # 8 to 128 printable ASCII characters
  }

  # Optional
  pg_version        = 18
  cpu               = "4"
  memory            = "32Gi"
  connectivity_mode = "nlb_internal"
  allowed_cidrs     = ["10.0.0.0/16"]

  branch_pg_settings = {
    max_connections = "200"
  }
  endpoint_pg_settings = {
    work_mem = "64MB"
  }
}
