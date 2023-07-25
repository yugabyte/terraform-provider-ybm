## Create an Azure VPC
resource "ybm_vpc" "example-vpc" {
  name  = "example-vpc"
  cloud = "AZURE"
  region_cidr_info = [
    {
      region = "eastus"
    }
  ]
}

variable "password" {
  type        = string
  description = "YSQL and YCQL Password."
  sensitive   = true
}


# Create single region cluster on Azure 
resource "ybm_cluster" "single_region_cluster" {
  cluster_name = "single-region-cluster"
  cloud_type   = "AZURE"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "eastus"
      num_nodes = 3
      vpc_id    = ybm_vpc.example-vpc.vpc_id # Azure requires a VPC
    }
  ]
  cluster_tier           = "PAID" # Azure only supports PAID tier
  cluster_allow_list_ids = []     # Optional
  fault_tolerance        = "ZONE"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50
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
  depends_on = [ybm_vpc.example-vpc]
}


# Create Private Service endpoint
resource "ybm_private_service_endpoint" "npsenonok-region" {
  cluster_id          = ybm_cluster.single_region_cluster.cluster_id
  region              = "eastus"
  security_principals = ["your_azure_subscriptions_id"]
  depends_on          = [ybm_cluster.single_region_cluster]
}
