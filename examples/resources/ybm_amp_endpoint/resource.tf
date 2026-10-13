# A read-only replica of the main branch
resource "ybm_amp_endpoint" "replica" {
  cluster_name  = ybm_amp_cluster.example.cluster_name
  endpoint_name = "replica"
  cpu           = "2"
  memory        = "16Gi"
}

# The read-write endpoint of a branch
resource "ybm_amp_endpoint" "dev" {
  cluster_name      = ybm_amp_cluster.example.cluster_name
  branch_name       = ybm_amp_branch.dev.branch_name
  endpoint_name     = "rw"
  role              = "read_write"
  connectivity_mode = "nlb_internal"
  allowed_cidrs     = ["10.0.0.0/16"]

  credentials = {
    password = var.dev_password
  }

  # Overrides the branch's settings for this endpoint
  pg_settings = {
    work_mem = "64MB"
  }
}
