resource "ybm_integration" "aws_s3" {
  config_name = "aws-s3-example"
  type        = "AWS_S3"
  aws_s3_spec = {
    bucket_name       = "<bucket-name>"
    region            = "us-west-2"
    access_key_id     = "<access-key-id>"
    secret_access_key = "<secret-access-key>"
    path_prefix       = "yugabyte-logs/"
  }
}
