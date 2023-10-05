# Cluster with single region

variable "password" {
  type        = string
  description = "YSQL Password."
  sensitive   = true
}

resource "ybm_vpc" "example-vpc" {
  name  = "example-vpc"
  cloud = "AWS"
  region_cidr_info = [
    {
      region = "us-east-1"
      cidr   = "10.231.0.0/24"
    }
  ]
}

resource "ybm_allow_list" "example_allow_list" {
  allow_list_name        = "allow-nobody"
  allow_list_description = "allow 192.168.0.1"
  cidr_list              = ["192.168.0.1/32"]
}


resource "ybm_cluster" "single_region_cluster" {
  cluster_name = "single-region-cluster"
  cloud_type   = "AWS"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region        = "us-east-1"
      num_nodes     = 1
      vpc_id        = ybm_vpc.example-vpc.vpc_id
      public_access = true
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = [ybm_allow_list.example_allow_list.allow_list_id]
  fault_tolerance        = "NONE"
  node_config = {
    num_cores    = 4
    disk_size_gb = 50
  }
  credentials = {
    username = "example_ysql_user"
    password = var.password
  }

}
