# EAR enabled single region cluster
# The same cmk_spec can be used for multi region/read replica clusters as well
# Encryption at rest is supported on clusters with database version 2.16.7.0 or later

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
  cluster_name = "test-cluster-with-aws-cmk"
  # The cloud provider for the cluster is indepedent of the CMK Provider
  # eg. GCP cluster with AWS CMK is supported
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 6
    }
  ]
  cluster_tier           = "PAID"
  # fault tolerance cannot be NONE for CMK enabled cluster
  fault_tolerance        = "ZONE"

  cmk_spec = {
    provider_type = "AWS"
    aws_cmk_spec = {
      access_key = "AKIAIOSFODNN7EXAMPLE"
      secret_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
      arn_list = [
        "arn:aws:kms:us-west-2:111122223333:key/1234abcd-12ab-34cd-56ef-1234567890ab"
      ]
    }
    is_enabled =  true
  }

  node_config = {
    num_cores    = 4
    disk_size_gb = 50
  }
  credentials = {
    ysql_username = "example_ysql_user"
    ysql_password = var.ysql_password
    ycql_username = "example_ycql_user"
    ycql_password = var.ycql_password
  }
}