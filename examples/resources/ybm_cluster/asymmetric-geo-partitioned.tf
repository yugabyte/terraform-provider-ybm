variable "password" {
  type        = string
  description = "YSQL and YCQL Password."
  sensitive   = true
}

# Asymmetric Geo Partitioned Cluster
# Creates 3 regions us-west1/asia-east1/europe-central2 with num_cores as 2/4/4 respectively
resource "ybm_cluster" "asymmetric_geo_partitioned_cluster" {
  cluster_name = "asymmetric-geo-partitioned-cluster"
  cloud_type   = "GCP"
  cluster_type = "GEO_PARTITIONED"
  cluster_region_info = [
    {
      region       = "us-west1"
      num_nodes    = 1
      num_cores    = 2
      disk_size_gb = 50               #Optional
      vpc_id       = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region       = "asia-east1"
      num_nodes    = 1
      num_cores    = 4
      disk_size_gb = 100              #Optional
      vpc_id       = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region       = "europe-central2"
      num_nodes    = 1
      num_cores    = 4
      disk_size_gb = 100              # Optional
      vpc_id       = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id-1", "example-allow-list-id-2"] #Optional
  restore_backup_id      = "example-backup-id"                                    #Optional
  fault_tolerance        = "REGION"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50 #Optional
  }
  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 10
      time_interval_in_days    = 10
    }
  ] #Optional
  credentials = {
    username = "example_user"
    password = var.password
  }
}