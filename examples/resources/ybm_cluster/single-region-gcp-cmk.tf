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
  cluster_name = "test-cluster-with-gcp-cmk"
  # The cloud provider for the cluster is indepedent of the CMK Provider
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
    provider_type = "GCP"
    gcp_cmk_spec = {
      location         = "global"
      key_ring_name    = "example_cmk_key_ring"
      key_name         = "example_cmk_key"
      protection_level = "software"
      gcp_service_account = {
        type                        = "service_account"
        project_id                  = "your-project-id"
        private_key_id              = "your-private-key-id"
        private_key                 = "-----BEGIN PRIVATE KEY-----\nYourPrivateRSAKey\n-----END PRIVATE KEY-----\n"
        client_email                = "your-service-account-email@your-project-id.iam.gserviceaccount.com"
        client_id                   = "your-client-id"
        auth_uri                    = "https://accounts.google.com/o/oauth2/auth"
        token_uri                   = "https://accounts.google.com/o/oauth2/token"
        auth_provider_x509_cert_url = "https://www.googleapis.com/oauth2/v1/certs"
        client_x509_cert_url        = "https://www.googleapis.com/.../your-service-account-email%40your-project-id.iam.gserviceaccount.com"
        universe_domain             = "googleapis.com"
    } }
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