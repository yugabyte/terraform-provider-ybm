variable "auth_token" {
  type        = string
  description = "The authentication token."
  sensitive = true
}

variable "account_id" {
  type        = string
  description = "The account ID."
}

resource "ybm_cluster" "single_region" {
  account_id = var.account_id
  cluster_name = "terraform-test-posriniv-2"
  cloud_type = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region = "us-west2"
      num_nodes = 1
      vpc_id = ybm_vpc.newvpc.vpc_id
    }
  ]
  cluster_tier = "PAID"
  cluster_allow_list_ids = [ybm_allow_list.mylist.allow_list_id]
  fault_tolerance = "NONE"
  node_config = {
    num_cores = 2
    memory_mb = 8192
    disk_size_gb = 10
  }
  // for custom_backup_schedule to be activated pass true 

  backup_schedule={
    state= "ACTIVE"
    retention_period_in_days=22
    time_interval_in_days=22
  }
  is_production = false
  credentials = {
    ysql_username = "ysql_user"
    ysql_password = "Password1"
    ycql_username = "ycql_user"
    ycql_password = "Password1"
  }
 
}