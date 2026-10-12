# A branch of a shared cluster, forked from main
resource "ybm_amp_branch" "dev" {
  cluster_name = ybm_amp_cluster.example.cluster_name
  branch_name  = "dev"

  # Optional; both can be changed in place, and extensions can only be added
  pg_settings = {
    work_mem = "128MB"
  }
  extensions = ["vector"]
}

# A branch forked from another branch at a given LSN
resource "ybm_amp_branch" "feature" {
  cluster_name   = ybm_amp_cluster.example.cluster_name
  branch_name    = "feature"
  parent_branch  = ybm_amp_branch.dev.branch_name
  fork_point_lsn = "0/16B3748"
}

# A branch of a cluster in a BYOC environment
resource "ybm_amp_branch" "byoc_dev" {
  environment_id = ybm_byoc_environment.example.environment_id
  cluster_name   = ybm_byoc_cluster.example.cluster_name
  branch_name    = "dev"
}
