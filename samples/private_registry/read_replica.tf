terraform {
  required_providers {
    ybm = {
      version = "0.1.0"
      source = "app.terraform.io/yugabyte/ybm"
    }
  }
}

provider "ybm" {
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

variable "ysql_password" {
  type        = string
  description = "YSQL Password."
  sensitive = true
}

variable "ycql_password" {
  type        = string
  description = "YCQL Password."
  sensitive = true
}

resource "ybm_cluster" "single_region" {
  account_id = var.account_id
  cluster_name = "terraform-cluster"
  cloud_type = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region = "us-west3"
      num_nodes = 1
      vpc_id = ybm_vpc.rrvpc.vpc_id
    }
  ]
  cluster_tier = "PAID"
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
  
  credentials = {
    ysql_username = "example_ysql_user"
    ysql_password = var.ysql_password
    ycql_username = "example_ycql_user"
    ycql_password = var.ycql_password
  }
 
}

resource "ybm_vpc" "rrvpc" {
  account_id = var.account_id
  name = "read-replica-vpc"
  cloud = "GCP"
  # Use only one among global cidr and region cidr
  global_cidr = "10.9.0.0/18"
}


resource "ybm_read_replicas" "rr" {
  account_id = var.account_id
  read_replicas_info = [ 
    {
      cloud_type = "GCP"
      num_replicas = 1
      num_nodes = 1
      region = "us-east4"
      vpc_id = ybm_vpc.rrvpc.vpc_id
      node_config = {
        num_cores = 2
        memory_mb = 8192
        disk_size_gb = 10
      }
    }
  ]
  primary_cluster_id = ybm_cluster.single_region.cluster_id
}
