variable "password" {
  type        = string
  description = "YSQL and YCQL Password."
  sensitive   = true
}

resource "ybm_cluster" "single_region_cluster" {
  cluster_name = "single-region-cluster"
  cloud_type   = "AZURE"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "eastus"
      num_nodes = 3
      vpc_id    = ybm_vpc.example-vpc.vpc_id # Azure requires a VPC
    }
  ]
  cluster_tier           = "PAID" # Azure only supports PAID tier
  cluster_allow_list_ids = [] # Optional
  fault_tolerance        = "ZONE"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50
  }
  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 10
      time_interval_in_days    = 10
    }
  ]  #Optional
  credentials = {
    username = "example_user"
    password = var.password
  }

}