# Autoscaler policy for a primary cluster region.
# Create a separate ybm_autoscaler_policy for each region or READ_REPLICA
# that requires its own autoscaling policy.
#
# Set status = "ACTIVE" to enable autoscaling, or "INACTIVE" to disable it.
#
# When status = "ACTIVE", set ignore_num_nodes_changes = true on the matching
# ybm_cluster cluster_region_info entry. This prevents autoscaler-driven runtime
# num_nodes changes from being treated as Terraform drift.
#
# Explicit num_nodes changes made in the Terraform configuration are still applied.
#
# Resizing outside the currently active autoscaling policy [min, max] requires
# two applies:
# 1) update the policy min/max and run terraform apply
# 2) update the cluster num_nodes and run terraform apply again
resource "ybm_autoscaler_policy" "example_autoscaler_policy" {
  cluster_id                               = "example-cluster-id"
  region                                   = "us-west1"
  type                                     = "PRIMARY"
  scalable_resource                        = "NODE"
  min                                      = 3
  max                                      = 9
  scale_in_cooldown_period_minutes         = 180
  scale_out_cooldown_period_minutes        = 180
  post_maintenance_cooldown_period_minutes = 180
  status                                   = "ACTIVE"

  policy_rules = [
    {
      scaling_type = "SCALE_OUT"
      clause       = "OR"
      rules = [
        {
          name              = "cpu-high"
          resource          = "CPU"
          condition         = "GT"
          value             = 80
          evaluation_window = "5m"
          scaling_action = {
            delta = 3
          }
        }
      ]
    },
    {
      scaling_type = "SCALE_IN"
      clause       = "AND"
      rules = [
        {
          name              = "cpu-low"
          resource          = "CPU"
          condition         = "LT"
          value             = 30
          evaluation_window = "5m"
          scaling_action = {
            delta = 3
          }
        }
      ]
    }
  ]
}

