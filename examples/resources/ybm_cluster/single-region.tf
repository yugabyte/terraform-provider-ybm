resource "ybm_cluster" "single_region_cluster" {
  account_id = "example-account-id"
  cluster_name = "single-region-cluster"
  cloud_type = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region = "us-west2"
      num_nodes = 1
      vpc_id = "example-vpc-id"
    }
  ]
  cluster_tier = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id-1","example-allow-list-id-2"]
  fault_tolerance = "NONE"
  node_config = {
    num_cores = 2
    memory_mb = 8192
    disk_size_gb = 10
  }
  backup_schedule={
    state= "ACTIVE"
    retention_period_in_days = 10
    time_interval_in_days = 10
  }
  is_production = false
  credentials = {
    ysql_username = "ysql_user"
    ysql_password = "Password1"
    ycql_username = "ycql_user"
    ycql_password = "Password1"
  }
 
}