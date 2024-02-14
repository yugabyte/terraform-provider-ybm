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
  cluster_name = "test-cluster-with-azure-cmk"
  # The cloud provider for the cluster is indepedent of the CMK Provider
  # eg. GCP cluster with AZURE CMK is supported
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 6
    }
  ]
  cluster_tier = "PAID"
  # fault tolerance cannot be NONE for CMK enabled cluster
  fault_tolerance = "ZONE"
  cmk_spec = {
    provider_type = "AZURE"
    azure_cmk_spec = {
      client_id     = "your-client-id"
      client_secret = "your-client-secret"
      tenant_id     = "your-tenant-id"
      key_name      = "your-key-name"
      key_vault_uri = "your-key-vault-uri"
    }
    is_enabled = true
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