variable "password" {
  type        = string
  description = "YSQL and YCQL Password."
  sensitive   = true
}

# Comprehensive Backup Replication Examples
# This file demonstrates different backup replication strategies for various cluster types

# 1. Single Region Cluster with Backup Replication
resource "ybm_cluster" "single_region_backup_replication" {
  cluster_name = "single-region-backup-replication"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region                        = "us-west1"
      num_nodes                     = 1
      num_cores                     = 2
      disk_size_gb                  = 50
      vpc_id                        = "example-vpc-id"
      public_access                 = true
      backup_replication_gcp_target = "single-region-backup-bucket"
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id"]
  fault_tolerance        = "NONE"

  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 30
      time_interval_in_days    = 7
    }
  ]

  credentials = {
    username = "example_user"
    password = var.password
  }
}

# 2. Multi-Region SYNCHRONOUS Cluster with Centralized Backup
# All regions backup to the same GCS bucket
resource "ybm_cluster" "multi_region_sync_centralized_backup" {
  cluster_name = "multi-region-sync-centralized-backup"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region                        = "us-west1"
      num_nodes                     = 1
      num_cores                     = 2
      disk_size_gb                  = 50
      vpc_id                        = "example-vpc-id-1"
      public_access                 = true
      backup_replication_gcp_target = "central-backup-bucket" # Same for all regions
    },
    {
      region                        = "us-central1"
      num_nodes                     = 1
      num_cores                     = 2
      disk_size_gb                  = 50
      vpc_id                        = "example-vpc-id-2"
      public_access                 = true
      backup_replication_gcp_target = "central-backup-bucket" # Same for all regions
    },
    {
      region                        = "us-east1"
      num_nodes                     = 1
      num_cores                     = 2
      disk_size_gb                  = 50
      vpc_id                        = "example-vpc-id-3"
      public_access                 = true
      backup_replication_gcp_target = "central-backup-bucket" # Same for all regions
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id"]
  fault_tolerance        = "REGION"

  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 30
      time_interval_in_days    = 7
    }
  ]

  credentials = {
    username = "example_user"
    password = var.password
  }
}

# 3. Multi-Region GEO_PARTITIONED Cluster with Region-Specific Backup
# Each region can have different backup targets for compliance or performance reasons
resource "ybm_cluster" "multi_region_geo_region_specific_backup" {
  cluster_name = "multi-region-geo-region-specific-backup"
  cloud_type   = "GCP"
  cluster_type = "GEO_PARTITIONED"
  cluster_region_info = [
    {
      region                        = "us-west1"
      num_nodes                     = 1
      num_cores                     = 2
      disk_size_gb                  = 50
      vpc_id                        = "example-vpc-id-1"
      public_access                 = true
      backup_replication_gcp_target = "us-west-backup-bucket" # Region-specific
    },
    {
      region                        = "asia-east1"
      num_nodes                     = 1
      num_cores                     = 2
      disk_size_gb                  = 50
      vpc_id                        = "example-vpc-id-2"
      public_access                 = true
      backup_replication_gcp_target = "asia-east-backup-bucket" # Region-specific
    },
    {
      region                        = "europe-central2"
      num_nodes                     = 1
      num_cores                     = 2
      disk_size_gb                  = 50
      vpc_id                        = "example-vpc-id-3"
      public_access                 = true
      backup_replication_gcp_target = "europe-central-backup-bucket" # Region-specific
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id"]
  fault_tolerance        = "REGION"

  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 30
      time_interval_in_days    = 7
    }
  ]

  credentials = {
    username = "example_user"
    password = var.password
  }
}

# 4. Asymmetric GEO_PARTITIONED Cluster with Mixed Backup Strategy
# Some regions share backup targets, others have unique targets
resource "ybm_cluster" "asymmetric_geo_mixed_backup" {
  cluster_name = "asymmetric-geo-mixed-backup"
  cloud_type   = "GCP"
  cluster_type = "GEO_PARTITIONED"
  cluster_region_info = [
    {
      region                        = "us-west1"
      num_nodes                     = 1
      num_cores                     = 2
      disk_size_gb                  = 50
      vpc_id                        = "example-vpc-id-1"
      public_access                 = true
      backup_replication_gcp_target = "us-west-backup-bucket"
    },
    {
      region                        = "us-central1"
      num_nodes                     = 1
      num_cores                     = 4
      disk_size_gb                  = 100
      vpc_id                        = "example-vpc-id-2"
      public_access                 = true
      backup_replication_gcp_target = "us-central-backup-bucket"
    },
    {
      region                        = "us-east1"
      num_nodes                     = 1
      num_cores                     = 4
      disk_size_gb                  = 100
      vpc_id                        = "example-vpc-id-3"
      public_access                 = true
      backup_replication_gcp_target = "us-central-backup-bucket" # Same as us-central1
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id"]
  fault_tolerance        = "REGION"

  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 30
      time_interval_in_days    = 7
    }
  ]

  credentials = {
    username = "example_user"
    password = var.password
  }
}

# Important Notes:
# 
# 1. backup_replication_gcp_target can ONLY be set when editing existing clusters
#    - It will be ignored during initial cluster creation
#    - To add backup replication, first create the cluster, then update the configuration
#
# 2. Cluster Type Rules:
#    - SYNCHRONOUS: All regions MUST have the same backup_replication_gcp_target
#    - GEO_PARTITIONED: Each region can have different backup_replication_gcp_target values. This allows for region-specific backup strategies and compliance requirements
#
# 3. Requirements:
#    - Only supported for GCP clusters
#    - Only supported for PAID tier clusters
#    - All regions must have backup_replication_gcp_target if any are provided
#
# 4. Use Cases:
#    - Centralized backup strategy (SYNCHRONOUS clusters)
#    - Region-specific compliance requirements (GEO_PARTITIONED clusters)
#    - Performance optimization by keeping backups close to data
#    - Disaster recovery planning with multiple backup locations
