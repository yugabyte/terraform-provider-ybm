# Example: ignore autoscaler-driven num_nodes drift for a region under autoscaling.
#
# When ybm_autoscaler_policy.status = "ACTIVE", set ignore_num_nodes_changes = true
# on the matching ybm_cluster region. This prevents autoscaler-driven runtime
# num_nodes changes from being treated as Terraform drift.
#
# Explicit num_nodes changes made in the Terraform configuration are still applied.
#
# Resizing outside the currently active autoscaling policy [min, max] requires
# two applies:
# 1) update the policy min/max and run terraform apply
# 2) update the cluster num_nodes and run terraform apply again
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
