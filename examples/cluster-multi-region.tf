resource "ybm_cluster" "multi_region" {
  account_id = var.account_id
  cluster_name = "terraform-test-posriniv-3"
  cloud_type = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region = "us-west2"
      num_nodes = 1
      vpc_id = ybm_vpc.newvpc.vpc_id
    },
    {
      region = "asia-east1"
      num_nodes = 1
      vpc_id = ybm_vpc.newvpc.vpc_id
    },
    {
      region = "europe-central2"
      num_nodes = 1
      vpc_id = ybm_vpc.newvpc.vpc_id
    }
  ]
  cluster_tier = "PAID"
  cluster_allow_list_ids = [ybm_allow_list.mylist.allow_list_id]
  restore_backup_id = ybm_backup.mybackup.backup_id
  node_config = {
    fault_tolerance = "REGION"
    num_cores = 2
    memory_mb = 8192
    disk_size_gb = 10
  }
  is_production = false
  credentials = {
    ysql_username = "ysql_user"
    ysql_password = "Password1"
    ycql_username = "ycql_user"
    ycql_password = "Password1"
  }
}


# # Multi Region Cluster
# resource "ybm_cluster" "multi_region_cluster" {
#   account_id = var.account_id
#   cluster_name = "multi-region-cluster"
#   cloud_type = "GCP"
#   cluster_type = "SYNCHRONOUS"
#   cluster_region_info = [
#     {
#       region = "us-west2"
#       num_nodes = 1
#       vpc_id = "example-vpc-id"
#     },
#     {
#       region = "asia-east1"
#       num_nodes = 1
#       vpc_id = "example-vpc-id"
#     },
#     {
#       region = "europe-central2"
#       num_nodes = 1
#       vpc_id = "example-vpc-id"
#     }
#   ]
#   cluster_tier = "PAID"
#   cluster_allow_list_ids = ["example-allow-list-id-1","example-allow-list-id-2"] # Optional
#   restore_backup_id = "example-backup-id" #Optional
#   node_config = {
#     fault_tolerance = "REGION"
#     num_cores = 2
#     memory_mb = 8192
#     disk_size_gb = 10
#   }
#   backup_schedule = {
#     state= "ACTIVE"
#     retention_period_in_days = 10
#     time_interval_in_days = 10
#   } 
#   is_production = false
#   credentials = {
#     ysql_username = "ysql_user"
#     ysql_password = "Password1"
#     ycql_username = "ycql_user"
#     ycql_password = "Password1"
#   }
# }