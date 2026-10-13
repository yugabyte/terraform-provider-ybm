# A shared PostgreSQL cluster in the account's AMP Managed storage pool
resource "ybm_amp_cluster" "example" {
  cluster_name = "orders"
  region       = "us-east-1"

  credentials = {
    password = var.amp_password # 8 to 128 printable ASCII characters
  }

  # Optional
  pg_version        = 18
  cpu               = "2"
  memory            = "16Gi" # at least 8Gi per vCPU; defaults to 8Gi per vCPU
  connectivity_mode = "nlb_internal"
  allowed_cidrs     = ["10.0.0.0/16"]

  # PostgreSQL settings of the main branch, for every endpoint on it
  branch_pg_settings = {
    max_connections = "200"
  }

  # PostgreSQL settings of the read-write endpoint only; they override branch_pg_settings
  endpoint_pg_settings = {
    work_mem = "64MB"
  }
}

output "amp_connection" {
  value = "postgresql://${ybm_amp_cluster.example.username}@${ybm_amp_cluster.example.host}:${ybm_amp_cluster.example.port}/${ybm_amp_cluster.example.database}"
}
