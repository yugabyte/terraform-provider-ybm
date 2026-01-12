terraform {
  required_providers {
    ybm = {
      source  = "local/yugabyte/ybm"
      version = "1.0.0"
    }
  }
}

# Configure the YugabyteDB Managed Provider
provider "ybm" {
  auth_token      = "eyJhbGciOiJIUzI1NiJ9**********************************************************************************************************************"
  host            = "cloud.yugabyte.com"
  use_secure_host = true
}

# Create an AWS S3 integration for PG logs export
resource "ybm_integration" "s3_logs_exporter" {
  config_name = "s3-pg-logs-exporter"
  type        = "AWS_S3"

  aws_s3_spec = {
    bucket_name        = "sushil.kumar-2"
    region             = "us-west-2" # Update this to match your bucket's region
    access_key_id      = "AKIA23J7F*********"
    secret_access_key  = "OzlCv3uE/kH*************************"
    path_prefix        = "terraform-7-jan/"
    file_prefix        = "terraform-7-jan"
    partition_strategy = "hour"
  }
}

# Note: In production, use variables or environment variables for sensitive data
# This example uses hardcoded values for testing purposes only

# Output the integration ID for use in other resources
output "s3_integration_id" {
  description = "The ID of the S3 integration"
  value       = ybm_integration.s3_logs_exporter.config_id
}
