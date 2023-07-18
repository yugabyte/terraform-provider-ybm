---
page_title: "ybm_cluster Resource - YugabyteDB Managed"
description: |-
  The resource to create a YugabyteDB cluster. Use this resource to create both
  single- and multi-region clusters. You can also use this resource to bind allow lists to the cluster
  being created; restore previously taken backups to the cluster being created;
  and modify the backup schedule of the cluster being created.
---

# ybm_cluster (Resource)

The resource to create a YugabyteDB cluster. Use this resource to create both 
single- and multi-region clusters. You can also use this resource to bind allow lists to the cluster 
being created; restore previously taken backups to the cluster being created; 
and modify the backup schedule of the cluster being created.


## Example Usage

To create a single region cluster by using common credentials for both YSQL and YCQL API

```terraform
variable "password" {
  type        = string
  description = "YSQL and YCQL Password."
  sensitive   = true
}

# Single Region Cluster
resource "ybm_cluster" "single_region_cluster" {
  cluster_name = "single-region-cluster"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 3
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id-1", "example-allow-list-id-2"] #Optional
  fault_tolerance        = "ZONE"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50 #Optional
  }
  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 10
      time_interval_in_days    = 10
    }
  ] #Optional
  credentials = {
    username = "example_user"
    password = var.password
  }

}
```

To create a single region cluster by using distinct credentials for both YSQL and YCQL API

```terraform
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

# Single Region Cluster
resource "ybm_cluster" "single_region_cluster" {
  cluster_name = "single-region-cluster"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 3
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id-1", "example-allow-list-id-2"] #Optional
  fault_tolerance        = "ZONE"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50 #Optional
  }
  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 10
      time_interval_in_days    = 10
    }
  ] #Optional
  credentials = {
    ysql_username = "example_ysql_user"
    ysql_password = var.ysql_password
    ycql_username = "example_ycql_user"
    ycql_password = var.ycql_password
  }

}
```

To create a multi region cluster by using common credentials for both YSQL and YCQL API

```terraform
variable "password" {
  type        = string
  description = "YSQL and YCQL Password."
  sensitive   = true
}

# Multi Region Cluster
resource "ybm_cluster" "multi_region_cluster" {
  cluster_name = "multi-region-cluster"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region    = "asia-east1"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region    = "europe-central2"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id-1", "example-allow-list-id-2"] #Optional
  restore_backup_id      = "example-backup-id"                                    #Optional
  fault_tolerance        = "REGION"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50 #Optional
  }
  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 10
      time_interval_in_days    = 10
    }
  ] #Optional
  credentials = {
    username = "example_user"
    password = var.password
  }
}
```

To create a multi region cluster by using distinct credentials for both YSQL and YCQL API

```terraform
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

# Multi Region Cluster
resource "ybm_cluster" "multi_region_cluster" {
  cluster_name = "multi-region-cluster"
  cloud_type   = "GCP"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "us-west1"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region    = "asia-east1"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    },
    {
      region    = "europe-central2"
      num_nodes = 1
      vpc_id    = "example-vpc-id" #Optional
      #vpc_name = "example-vpc-name" #Optional You can also use the VPC Name in place of vpc_id
    }
  ]
  cluster_tier           = "PAID"
  cluster_allow_list_ids = ["example-allow-list-id-1", "example-allow-list-id-2"] #Optional
  restore_backup_id      = "example-backup-id"                                    #Optional
  fault_tolerance        = "REGION"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50 #Optional
  }
  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 10
      time_interval_in_days    = 10
    }
  ] #Optional
  credentials = {
    ysql_username = "example_ysql_user"
    ysql_password = var.ysql_password
    ycql_username = "example_ycql_user"
    ycql_password = var.ycql_password
  }
}
```

To create an AWS Cluster with Customer Managed Keys

```terraform
# EAR enabled single region cluster
# The same cmk_spec can be used for multi region/read replica clusters as well

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
```

To create a GCP Cluster with Customer Managed Keys

```terraform
# EAR enabled single region cluster
# The same cmk_spec can be used for multi region/read replica clusters as well

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
  cluster_tier           = "PAID"
  # fault tolerance cannot be NONE for CMK enabled cluster
  fault_tolerance        = "ZONE"

  cmk_spec = {
    provider_type = "GCP"
    gcp_cmk_spec = {
    location = "global"
    key_ring_name = "example_cmk_key_ring"
    key_name = "example_cmk_key"
    protection_level = "software"
    gcp_service_account = {
        type = "service_account"
        project_id = "your-project-id"
        private_key_id = "your-private-key-id"
        private_key = "-----BEGIN PRIVATE KEY-----\nYourPrivateRSAKey\n-----END PRIVATE KEY-----\n"
        client_email = "your-service-account-email@your-project-id.iam.gserviceaccount.com"
        client_id = "your-client-id"
        auth_uri = "https://accounts.google.com/o/oauth2/auth"
        token_uri = "https://accounts.google.com/o/oauth2/token"
        auth_provider_x509_cert_url = "https://www.googleapis.com/oauth2/v1/certs"
        client_x509_cert_url = "https://www.googleapis.com/.../your-service-account-email%40your-project-id.iam.gserviceaccount.com"
        universe_domain = "googleapis.com"
    }}
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
```

To create an Azure Cluster

```terraform
variable "password" {
  type        = string
  description = "YSQL and YCQL Password."
  sensitive   = true
}

resource "ybm_cluster" "single_region_cluster" {
  cluster_name = "single-region-cluster"
  cloud_type   = "AZURE"
  cluster_type = "SYNCHRONOUS"
  cluster_region_info = [
    {
      region    = "eastus"
      num_nodes = 3
      vpc_id    = ybm_vpc.example-vpc.vpc_id # Azure requires a VPC
      #vpc_name = "example-vpc-name" # You can also use the VPC Name in place of vpc_id
    }
  ]
  cluster_tier           = "PAID" # Azure only supports PAID tier
  cluster_allow_list_ids = []     # Optional
  fault_tolerance        = "ZONE"
  node_config = {
    num_cores    = 2
    disk_size_gb = 50
  }
  backup_schedules = [
    {
      state                    = "ACTIVE"
      retention_period_in_days = 10
      time_interval_in_days    = 10
    }
  ] #Optional
  credentials = {
    username = "example_user"
    password = var.password
  }

}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `cluster_name` (String) The name of the cluster.
- `cluster_region_info` (Attributes List) (see [below for nested schema](#nestedatt--cluster_region_info))
- `cluster_tier` (String) FREE (Sandbox) or PAID (Dedicated).
- `cluster_type` (String) The type of the cluster. SYNCHRONOUS or GEO_PARTITIONED
- `credentials` (Attributes) Credentials to be used by the database. Please provide 'username' and 'password' 
(which would be used in common for both YSQL and YCQL) OR all of 'ysql_username',
'ysql_password', 'ycql_username' and 'ycql_password' but not a mix of both. (see [below for nested schema](#nestedatt--credentials))
- `node_config` (Attributes) (see [below for nested schema](#nestedatt--node_config))

### Optional

- `backup_schedules` (Attributes List) (see [below for nested schema](#nestedatt--backup_schedules))
- `cloud_type` (String) The cloud provider where the cluster is deployed: AWS, AZURE or GCP.
- `cluster_allow_list_ids` (List of String) List of IDs of the allow lists assigned to the cluster.
- `cluster_endpoints` (Map of String) The endpoints used to connect to the cluster by region.
- `cluster_id` (String) The ID of the cluster. Created automatically when a cluster is created. Used to get a specific cluster.
- `cmk_spec` (Attributes) KMS Provider Configuration. (see [below for nested schema](#nestedatt--cmk_spec))
- `database_track` (String) The track of the database. Production or Innovation or Preview.
- `desired_state` (String) The desired state of the database, Active or Paused. This parameter can be used to pause/resume a cluster.
- `fault_tolerance` (String) The fault tolerance of the cluster. NONE, NODE, ZONE or REGION.
- `restore_backup_id` (String) The ID of the backup to be restored to the cluster.

### Read-Only

- `account_id` (String) The ID of the account this cluster belongs to.
- `cluster_certificate` (String) The certificate used to connect to the cluster.
- `cluster_info` (Attributes) (see [below for nested schema](#nestedatt--cluster_info))
- `cluster_version` (String)
- `project_id` (String) The ID of the project this cluster belongs to.

<a id="nestedatt--cluster_region_info"></a>
### Nested Schema for `cluster_region_info`

Required:

- `num_nodes` (Number)
- `region` (String)

Optional:

- `vpc_id` (String)
- `vpc_name` (String)


<a id="nestedatt--credentials"></a>
### Nested Schema for `credentials`

Optional:

- `password` (String, Sensitive) The password to be used for both YSQL and YCQL. Note that this will be stored in the state file.
- `username` (String) The username to be used for both YSQL and YCQL.
- `ycql_password` (String, Sensitive) YCQL password for the database. Note that this will be stored in the state file.
- `ycql_username` (String) YCQL username for the database.
- `ysql_password` (String, Sensitive) YSQL password for the database. Note that this will be stored in the state file.
- `ysql_username` (String) YSQL username for the database.


<a id="nestedatt--node_config"></a>
### Nested Schema for `node_config`

Required:

- `num_cores` (Number) Number of CPU cores in the node.

Optional:

- `disk_iops` (Number) Disk IOPS of the node.
- `disk_size_gb` (Number) Disk size of the node.


<a id="nestedatt--backup_schedules"></a>
### Nested Schema for `backup_schedules`

Optional:

- `backup_description` (String) The description of the backup schedule.
- `cron_expression` (String) The cron expression for the backup schedule
- `retention_period_in_days` (Number) The retention period of the backup schedule.
- `schedule_id` (String) The ID of the backup schedule. Created automatically when the backup schedule is created. Used to get a specific backup schedule.
- `state` (String) The state of the backup schedule. Used to pause or resume the backup schedule. Valid values are ACTIVE or PAUSED.
- `time_interval_in_days` (Number) The time interval in days for the backup schedule.


<a id="nestedatt--cmk_spec"></a>
### Nested Schema for `cmk_spec`

Required:

- `is_enabled` (Boolean) Is Enabled
- `provider_type` (String) CMK Provider Type.

Optional:

- `aws_cmk_spec` (Attributes) AWS CMK Provider Configuration. (see [below for nested schema](#nestedatt--cmk_spec--aws_cmk_spec))
- `gcp_cmk_spec` (Attributes) GCP CMK Provider Configuration. (see [below for nested schema](#nestedatt--cmk_spec--gcp_cmk_spec))

<a id="nestedatt--cmk_spec--aws_cmk_spec"></a>
### Nested Schema for `cmk_spec.aws_cmk_spec`

Required:

- `access_key` (String) Access Key
- `arn_list` (List of String) AWS ARN List
- `secret_key` (String) Secret Key


<a id="nestedatt--cmk_spec--gcp_cmk_spec"></a>
### Nested Schema for `cmk_spec.gcp_cmk_spec`

Required:

- `gcp_service_account` (Attributes) GCP Service Account (see [below for nested schema](#nestedatt--cmk_spec--gcp_cmk_spec--gcp_service_account))
- `key_name` (String) Key Name
- `key_ring_name` (String) Key Ring Name
- `location` (String) Location
- `protection_level` (String) Key Protection Level

<a id="nestedatt--cmk_spec--gcp_cmk_spec--gcp_service_account"></a>
### Nested Schema for `cmk_spec.gcp_cmk_spec.gcp_service_account`

Required:

- `auth_provider_x509_cert_url` (String) Auth Provider X509 Cert URL
- `auth_uri` (String) Auth URI
- `client_email` (String) Client Email
- `client_id` (String) Client ID
- `client_x509_cert_url` (String) Client X509 Cert URL
- `private_key` (String) Private Key
- `private_key_id` (String) Private Key ID
- `project_id` (String) GCP Project ID
- `token_uri` (String) Token URI
- `type` (String) Service Account Type

Optional:

- `universe_domain` (String) Google Universe Domain




<a id="nestedatt--cluster_info"></a>
### Nested Schema for `cluster_info`

Read-Only:

- `created_time` (String)
- `software_version` (String)
- `state` (String)
- `updated_time` (String)
