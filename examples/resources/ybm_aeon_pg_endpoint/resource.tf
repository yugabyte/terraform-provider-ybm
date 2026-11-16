# The read-write endpoint on main, which sets the cluster's password
resource "ybm_aeon_pg_endpoint" "primary" {
  cluster_name  = ybm_aeon_pg_cluster.example.cluster_name
  cluster_id    = ybm_aeon_pg_cluster.example.cluster_id
  endpoint_name = "primary"
  role          = "read_write"

  credentials = {
    password = var.pg_password # 8 to 128 printable ASCII characters
  }
}

# A read-only replica of main
resource "ybm_aeon_pg_endpoint" "replica" {
  cluster_name  = ybm_aeon_pg_cluster.example.cluster_name
  cluster_id    = ybm_aeon_pg_cluster.example.cluster_id
  endpoint_name = "replica"
  cpu           = "2"
  memory        = "16Gi"
}

# The read-write endpoint of a branch, which uses the cluster's password
resource "ybm_aeon_pg_endpoint" "dev" {
  cluster_name      = ybm_aeon_pg_cluster.example.cluster_name
  cluster_id        = ybm_aeon_pg_cluster.example.cluster_id
  branch_name       = ybm_aeon_pg_branch.dev.branch_name
  endpoint_name     = "rw"
  role              = "read_write"
  connectivity_mode = "nlb_internal"
  allowed_cidrs     = ["10.0.0.0/16"]

  # Overrides the branch's settings for this endpoint
  pg_settings = {
    work_mem = "64MB"
  }
}
