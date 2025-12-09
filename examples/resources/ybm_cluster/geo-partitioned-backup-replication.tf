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

# GEO_PARTITIONED Cluster with Region-Specific Backup Replication
# Each region can have its own backup replication target
resource "ybm_cluster" "geo_partitioned_backup_replication" {
  cluster_name = "geo-partitioned-backup-replication"
  cloud_type   = "GCP"
  cluster_type = "GEO_PARTITIONED"
  cluster_tier = "PAID"

  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 1
      vpc_id    = "example-vpc-id-1"
      num_cores = 2
    },
    {
      region    = "asia-east1"
      num_nodes = 1
      vpc_id    = "example-vpc-id-2"
      num_cores = 2
    },
    {
      region    = "europe-central2"
      num_nodes = 1
      vpc_id    = "example-vpc-id-3"
      num_cores = 2
    }
  ]

  cluster_allow_list_ids = ["example-allow-list-id"]
  fault_tolerance        = "REGION"

  # Backup replication configuration
  # For GEO_PARTITIONED clusters, each region can have its own backup target
  backup_replication_spec = {
    gcp_spec = {
      enabled = true
      geo_partitioned_cluster_spec = {
        replication_configs = [
          {
            desired_region = "us-west1"
            target         = "us-west-backup-bucket"
          },
          {
            desired_region = "asia-east1"
            target         = "asia-east-backup-bucket"
          },
          {
            desired_region = "europe-central2"
            target         = "europe-central-backup-bucket"
          }
        ]
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
