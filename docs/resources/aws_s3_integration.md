---
page_title: "ybm_integration Resource - AWS S3 Integration"
subcategory: ""
description: |-
  The integration resource allows you to create and manage AWS S3 integrations for PostgreSQL logs export in YugabyteDB Managed.
---

# ybm_integration (AWS S3 Integration)

Use this resource to create an AWS S3 integration for exporting PostgreSQL logs from your YugabyteDB clusters.

**Note:** This feature requires the S3 integration feature flag to be enabled. Set the environment variable `YBM_FF_S3_INTEGRATION=true` to enable this functionality.

## Example Usage

```bash
# Enable the S3 integration feature flag
export YBM_FF_S3_INTEGRATION=true
```

```terraform
resource "ybm_integration" "s3_logs_exporter" {
  config_name = "s3-pg-logs-exporter"
  type        = "AWS_S3"

  aws_s3_spec = {
    bucket_name         = "my-yugabyte-logs-bucket"
    region              = "us-west-2"
    access_key_id       = var.aws_access_key_id
    secret_access_key   = var.aws_secret_access_key
    path_prefix         = "yugabyte-logs/"
    file_prefix         = "yugabyte-logs"
    partition_strategy  = "hour"
  }
}
```

## Schema

### Required

- `config_name` (String) The name of the integration configuration
- `type` (String) Must be set to `"AWS_S3"` for S3 integrations
- `aws_s3_spec` (Block) AWS S3 configuration specifications (see [below for nested schema](#nestedblock--aws_s3_spec))

### Read-Only

- `account_id` (String) The ID of the account this integration belongs to
- `project_id` (String) The ID of the project this integration belongs to
- `config_id` (String) The ID of the integration
- `is_valid` (Boolean) Indicates whether the integration configuration is valid

<a id="nestedblock--aws_s3_spec"></a>
### Nested Schema for `aws_s3_spec`

#### Required

- `bucket_name` (String) The S3 bucket name to export logs to. Must be 3-63 characters and follow S3 bucket naming conventions.
- `region` (String) AWS region where the S3 bucket is located (e.g., "us-west-2")
- `access_key_id` (String, Sensitive) AWS Access Key ID for S3 access (16-128 characters)
- `secret_access_key` (String, Sensitive) AWS Secret Access Key for S3 access (40-128 characters)

#### Optional

- `path_prefix` (String) S3 path prefix for organizing objects in directories. Default: "yugabyte-logs/". Maximum 200 characters.
- `file_prefix` (String) Prefix for exported file names. Default: "yugabyte-logs". Maximum 50 characters.
- `partition_strategy` (String) Time-based partitioning strategy. Valid values: "minute", "hour". Default: "hour".

## S3 Path Structure

The final S3 path structure will be:
```
{path_prefix}/{cluster-id}/{node-name}/{partition}/
```

For example, with default settings:
```
yugabyte-logs/cluster-123/node-1/2024/01/07/14/
```

## Required IAM Permissions

Your AWS credentials must have the following IAM permissions for the S3 bucket:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Action": ["s3:PutObject"],
            "Effect": "Allow",
            "Resource": ["arn:aws:s3:::your-bucket-name/*"]
        }
    ]
}
```

## Import

Integrations can be imported using the integration ID:

```shell
terraform import ybm_integration.s3_logs_exporter integration-id-here
```

## Notes

- **Security**: The `access_key_id` and `secret_access_key` are marked as sensitive and will not be displayed in Terraform logs or state files.
- **Validation**: S3 bucket existence is not validated during configuration. Ensure the bucket exists and is accessible before applying.
- **Immutable**: Once created, integration specifications cannot be modified. You must destroy and recreate the resource to make changes.
- **API Support**: This feature requires API server support for S3 telemetry providers. Check with your YugabyteDB Managed administrator if you encounter issues.
