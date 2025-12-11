variable "ysql_password" {
  type        = string
  description = "YSQL Password."
  sensitive   = true
}

variable "ycql_password" {
  type        = string
  description = "YCQL Password."
  sensitive   = true
}

# Single Region SYNCHRONOUS Cluster with Backup Replication
# This example shows how to configure backup replication for a single-region cluster
resource "ybm_cluster" "single_region_backup_replication" {
  cluster_name = "single-region-backup-replication"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_tier = "PAID"

  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 3
      vpc_id    = "example-vpc-id"
      num_cores = 2
    }
  ]

  cluster_allow_list_ids = ["example-allow-list-id"]
  fault_tolerance        = "ZONE"

  # Backup replication configuration
  backup_replication_spec = {
    gcp_spec = {
      enabled = true
      sync_cluster_spec = {
        replication_config = {
          target = "my-backup-bucket"
        }
      }
    }
  }

  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 30
      time_interval_in_days    = 7
    }
  ]

  credentials = {
    ysql_username = "example_ysql_user"
    ysql_password = var.ysql_password
    ycql_username = "example_ycql_user"
    ycql_password = var.ycql_password
  }
}
