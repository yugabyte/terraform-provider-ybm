terraform {
  required_providers {
    yb = {
      version = "~> 0.1.1"
      source = "yugabyte/managed/yugabytedb-managed"
    }
  }
}

provider "yb" {
  host = "devcloud.yugabyte.com"
  use_secure_host = true
  auth_token = var.auth_token
}

variable "auth_token" {
  type        = string
  description = "The authentication token."
  sensitive = true
}

variable "account_id" {
  type        = string
  description = "The account ID."
}

resource "yb_cluster" "single_region" {
  account_id = var.account_id
  cluster_name = "terraform-test-posriniv-2"
  cloud_type = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region = "us-west2"
      num_nodes = 1
      vpc_id = yb_vpc.newvpc.vpc_id
    }
  ]
  cluster_tier = "PAID"
  cluster_allow_list_ids = [yb_allow_list.mylist.allow_list_id]
  fault_tolerance = "NONE"
  node_config = {
    num_cores = 2
    memory_mb = 8192
    disk_size_gb = 10
  }
  // for custom_backup_schedule to be activated pass true 

  backup_schedule={
    state= "ACTIVE"
    retention_period_in_days=22
    time_interval_in_days=22
  }
  is_production = false
  credentials = {
    ysql_username = "ysql_user"
    ysql_password = "Password1"
    ycql_username = "ycql_user"
    ycql_password = "Password1"
  }
 
}

data "yb_cluster" "clustername"{

  cluster_name= "terraform-test-posriniv-1"
  account_id= var.account_id
} 

resource "yb_allow_list" "mylist" {
  account_id = var.account_id
  allow_list_name = "all"
  allow_list_description = "allow all the ip addresses"
  cidr_list = ["0.0.0.0/0"]  
}

# data "yb_backup" "latest_backup" {
#   account_id = var.account_id
#   cluster_id = yb_cluster.single_region.cluster_id
#   most_recent = true
#   #Ensure the timestamp is in the format given below
#   #timestamp = 2022-07-08T00:06:01.890Z
# }

# resource "yb_backup" "mybackup" {
#   account_id = var.account_id
#   cluster_id = yb_cluster.single_region.cluster_id
#   backup_description = "backup"
#   retention_period_in_days = 2  
# }


resource "yb_vpc" "newvpc" {
  account_id = var.account_id
  name = "terraform-vpc"
  cloud = "GCP"
  # Use only one among global cidr and region cidr
  global_cidr = "10.9.0.0/18"
  # region_cidr_info = [
  #   {
  #     region = "europe-central2"
  #     cidr = "10.231.0.0/24"
  #   },
  #   {
  #     region = "us-west2" 
  #     cidr = "10.9.0.0/24"
  #   }
  # ]
}


resource "yb_read_replicas" "myrr" {
  account_id = var.account_id
  read_replicas_info = [ 
    {
      cloud_type = "GCP"
      num_replicas = 1
      num_nodes = 1
      region = "us-east4"
      vpc_id = yb_vpc.newvpc.vpc_id
      node_config = {
        num_cores = 2
        memory_mb = 8192
        disk_size_gb = 10
      }
    }
  ]
  primary_cluster_id = yb_cluster.single_region.cluster_id
}
