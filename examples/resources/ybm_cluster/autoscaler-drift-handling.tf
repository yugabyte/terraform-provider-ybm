# Example: ignore autoscaler-driven num_nodes drift for a region under autoscaling.
# When ybm_autoscaler_policy.status = "ACTIVE", set ignore_num_nodes_changes = true
# on the matching ybm_cluster region. Otherwise Terraform will detect drift after
# the autoscaler changes the runtime node count.
resource "ybm_cluster" "example_autoscaler_drift" {
  cluster_name    = "example-autoscaler-drift"
  cluster_type    = "SYNCHRONOUS"
  cloud_type      = "GCP"
  cluster_tier    = "PAID"
  fault_tolerance = "NODE"
  database_track  = "Stable"
  cluster_region_info = [
    {
      region                   = "us-west1"
      num_nodes                = 3
      ignore_num_nodes_changes = true
      num_cores                = 4
      disk_size_gb             = 50
    },
    {
      region    = "asia-east1"
      num_nodes = 1
      num_cores = 4
    }
  ]
}

resource "ybm_autoscaler_policy" "example_us_west1" {
  cluster_id = ybm_cluster.example_autoscaler_drift.cluster_id
  region     = "us-west1"
  type       = "PRIMARY"
  status     = "ACTIVE"
  min        = 3
  max        = 9
}
