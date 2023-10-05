variable "password" {
  type        = string
  description = "YSQL and YCQL Password."
  sensitive   = true
}

# Multi Region Cluster
resource "ybm_cluster" "multi_region_cluster" {
  cluster_name = "multi-region-cluster"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region    = "asia-east1"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region    = "europe-central2"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region    = "us-east1"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region    = "us-west4"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    }
    
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id-1", "example-allow-list-id-2"] #Optional
  restore_backup_id      = "example-backup-id"                                    #Optional
  fault_tolerance        = "REGION"
  num_faults_to_tolerate = 2
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

