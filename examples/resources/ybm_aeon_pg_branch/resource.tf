# A branch of a shared cluster, forked from main
resource "ybm_aeon_pg_branch" "dev" {
  cluster_name = ybm_aeon_pg_cluster.example.cluster_name
  cluster_id   = ybm_aeon_pg_cluster.example.cluster_id
  branch_name  = "dev"

  # Optional; both can be changed in place, and extensions can only be added
  pg_settings = {
    work_mem = "128MB"
  }
  extensions = ["vector"]
}

# A branch forked from another branch at a given LSN
resource "ybm_aeon_pg_branch" "feature" {
  cluster_name   = ybm_aeon_pg_cluster.example.cluster_name
  cluster_id     = ybm_aeon_pg_cluster.example.cluster_id
  branch_name    = "feature"
  parent_branch  = ybm_aeon_pg_branch.dev.branch_name
  fork_point_lsn = "0/16B3748"
}

# A branch of a cluster in a BYOC environment
resource "ybm_aeon_pg_branch" "byoc_dev" {
  environment_id = ybm_aeon_pg_byoc_cluster.example.environment_id
  storage_pool   = ybm_aeon_pg_byoc_cluster.example.storage_pool
  cluster_name   = ybm_aeon_pg_byoc_cluster.example.cluster_name
  cluster_id     = ybm_aeon_pg_byoc_cluster.example.cluster_id
  branch_name    = "dev"
}
