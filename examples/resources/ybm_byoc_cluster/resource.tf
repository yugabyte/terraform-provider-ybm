resource "ybm_byoc_cluster" "example" {
  environment_id = ybm_byoc_environment.example.environment_id
  cluster_name   = "orders"
  password       = var.byoc_password # 8 to 128 printable ASCII characters

  # Optional
  pg_version        = 18
  cpu               = "4"
  memory            = "32Gi"
  connectivity_mode = "nlb_internal"
  allowed_cidrs     = ["10.0.0.0/16"]

  # PostgreSQL settings of the read-write endpoint; can be changed in place
  pg_settings = {
    work_mem        = "64MB"
    max_connections = "200"
  }
}
