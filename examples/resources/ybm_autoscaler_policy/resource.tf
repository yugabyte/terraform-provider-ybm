# Autoscaler policy for a primary cluster region.
# Set status = "ACTIVE" to enable autoscaling in the region, or
# status = "INACTIVE" to disable it. Update status and apply to toggle.
resource "ybm_autoscaler_policy" "example_autoscaler_policy" {
  cluster_id = "example-cluster-id"

  clusters = [
    {
      cluster_id                               = "example-cluster-id"
      type                                     = "PRIMARY"
      scale_in_cooldown_period_minutes         = 180
      scale_out_cooldown_period_minutes        = 180
      post_maintenance_cooldown_period_minutes = 180

      regions = [
        {
          code   = "us-west1"
          status = "ACTIVE"
          policies = [
            {
              scalable_resource = "NODE"
              min               = 3
              max               = 9
              scaling_type      = "SCALE_OUT"
              clause            = "AND"
              rules = [
                {
                  name              = "cpu-high"
                  resource          = "CPU"
                  condition         = "GT"
                  value             = 80
                  evaluation_window = "5m"
                  scaling_action = {
                    delta = 1
                  }
                }
              ]
            },
            {
              scalable_resource = "NODE"
              min               = 3
              max               = 9
              scaling_type      = "SCALE_IN"
              clause            = "AND"
              rules = [
                {
                  name              = "cpu-low"
                  resource          = "CPU"
                  condition         = "LT"
                  value             = 30
                  evaluation_window = "5m"
                  scaling_action = {
                    delta = 1
                  }
                }
              ]
            }
          ]
        }
      ]
    }
  ]
}
