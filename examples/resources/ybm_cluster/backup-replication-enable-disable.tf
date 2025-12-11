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

# Example: Cluster with Backup Replication Initially Disabled
# You can enable backup replication later by setting enabled = true
resource "ybm_cluster" "cluster_with_backup_replication_disabled" {
  cluster_name = "cluster-backup-replication-disabled"
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

  # Backup replication is disabled
  backup_replication_spec = {
    gcp_spec = {
      enabled = false
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

# Example: Enable Backup Replication Later
# To enable backup replication, update the configuration:
# 1. Set enabled = true
# 2. Provide the sync_cluster_spec or geo_partitioned_cluster_spec with target bucket
resource "ybm_cluster" "cluster_with_backup_replication_enabled" {
  cluster_name = "cluster-backup-replication-enabled"
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

  # Backup replication is enabled with target bucket
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

# Example: Cluster without backup_replication_spec block
# If you don't specify backup_replication_spec, backup replication will not be configured
resource "ybm_cluster" "cluster_without_backup_replication" {
  cluster_name = "cluster-without-backup-replication"
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

  # No backup_replication_spec block - backup replication is not configured

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
