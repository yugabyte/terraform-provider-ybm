terraform {
  required_providers {
    ybm = {
      version = "0.1.0-dev"
      source  = "registry.terraform.io/yugabyte/ybm"
    }
  }
}

provider "ybm" {
  host            = "devcloud.yugabyte.com"
  use_secure_host = true
  auth_token      = var.auth_token
}

variable "auth_token" {
  type        = string
  description = "The authentication token."
  sensitive   = true
}

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

resource "ybm_cluster" "single_region" {
  cluster_name = "terraform-test-posriniv-2"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "us-west2"
      num_nodes = 1
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = [ybm_allow_list.mylist.allow_list_id]
  fault_tolerance        = "NONE"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50
  }
  // for custom_backup_schedule to be activated pass true 

  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 8
      time_interval_in_days    = 10
      incremental_interval_in_mins = 30
    }
  ]
  credentials = {
    ysql_username = "example_ysql_user"
    ysql_password = var.ysql_password
    ycql_username = "example_ycql_user"
    ycql_password = var.ycql_password
  }

}

# data "ybm_cluster" "clustername" {
#   cluster_name = "terraform-test-posriniv-1"
# }

resource "ybm_allow_list" "mylist" {
  allow_list_name        = "all"
  allow_list_description = "allow all the ip addresses"
  cidr_list              = ["0.0.0.0/0"]
}

# data "ybm_backup" "latest_backup" {
#   cluster_id = ybm_cluster.single_region.cluster_id
#   most_recent = true
#   #Ensure the timestamp is in the format given below
#   #timestamp = 2022-07-08T00:06:01.890Z
# }

# resource "ybm_backup" "mybackup" {
#   cluster_id = ybm_cluster.single_region.cluster_id
#   backup_description = "backup"
#   retention_period_in_days = 2  
# }


# resource "ybm_vpc" "newvpc" {
#   name       = "terraform-vpc"
#   cloud      = "GCP"
#   # Use only one among global cidr and region cidr
#   global_cidr = "10.9.0.0/18"
#   # region_cidr_info = [
#   #   {
#   #     region = "europe-central2"
#   #     cidr = "10.231.0.0/24"
#   #   },
#   #   {
#   #     region = "us-west2" 
#   #     cidr = "10.9.0.0/24"
#   #   }
#   # ]
# }


# resource "ybm_read_replicas" "myrr" {
#   read_replicas_info = [
#     {
#       cloud_type   = "GCP"
#       num_replicas = 1
#       num_nodes    = 1
#       region       = "us-east4"
#       vpc_id       = ybm_vpc.newvpc.vpc_id
#       node_config = {
#         num_cores    = 2
#         disk_size_gb = 10
#       }
#     }
#   ]
#   primary_cluster_id = ybm_cluster.single_region.cluster_id
# }

# resource "ybm_vpc_peering" "example_vpc_peering" {
#   name              = "example_name"
#   yugabytedb_vpc_id = "example_vpc_id"
#   application_vpc_info = {
#     cloud   = "GCP"
#     project = "example_project"
#     region  = "us-west1"
#     vpc_id  = "application_vpc_id"
#     cidr    = "example_cidr"
#   }
# }
