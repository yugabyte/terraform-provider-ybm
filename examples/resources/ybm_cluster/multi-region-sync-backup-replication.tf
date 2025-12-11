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

# Multi-Region SYNCHRONOUS Cluster with Backup Replication
# For SYNCHRONOUS clusters, a backup region is automatically assigned
resource "ybm_cluster" "multi_region_sync_backup_replication" {
  cluster_name = "multi-region-sync-backup-replication"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_tier = "PAID"

  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 1
      vpc_id    = "example-vpc-id-1"
      num_cores = 2
    },
    {
      region    = "us-central1"
      num_nodes = 1
      vpc_id    = "example-vpc-id-2"
      num_cores = 2
    },
    {
      region    = "us-east1"
      num_nodes = 1
      vpc_id    = "example-vpc-id-3"
      num_cores = 2
    }
  ]

  cluster_allow_list_ids = ["example-allow-list-id"]
  fault_tolerance        = "REGION"

  # Backup replication configuration
  # For SYNCHRONOUS clusters, all regions backup to the same GCS bucket present in the "backup region"
  backup_replication_spec = {
    gcp_spec = {
      enabled = true
      sync_cluster_spec = {
        replication_config = {
          target = "centralized-backup-bucket"
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
