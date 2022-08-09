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
  credentials = {
    ysql_username = "example_ysql_user"
    ysql_password = var.ysql_password
    ycql_username = "example_ycql_user"
    ycql_password = var.ycql_password
  }
}
