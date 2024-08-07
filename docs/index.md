---
page_title: YugabyteDB Aeon Provider
description: |-
  YugabyteDB Aeon Terraform Provider
---

# YugabyteDB Aeon Provider

[YugabyteDB](https://github.com/yugabyte/yugabyte-db) is a high-performance, cloud-native distributed SQL database that aims to support all PostgreSQL
features. It is best to fit for cloud-native OLTP (i.e. real-time, business-critical) applications that need absolute
data correctness and require at least one of the following: scalability, high tolerance to failures, or
globally-distributed deployments. [YugabyteDB Aeon](https://www.yugabyte.com/managed/) is a fully managed YugabyteDB-as-a-Service without
the operational overhead of managing a database.  

The YugabyteDB Aeon Provider can be used to interact with the resources provided by YugabyteDB Aeon like the YugabyteDB Clusters, Allow lists, VPCs,
VPC Peerings, Read Replicas and so on. The provider needs to be configured with appropriate credentials before it can base used. The navigation bar on the left
hand side provides the details about all the resources supported by the provider and the guides to use the provider.

## Example Usage

```terraform
variable "auth_token" {
  type        = string
  description = "The authentication token."
  sensitive   = true
}

provider "ybm" {
  host            = "cloud.yugabyte.com"
  use_secure_host = false # True by default
  auth_token      = var.auth_token
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `auth_token` (String, Sensitive) The authentication token (API key) of the account this cluster belongs to.
- `host` (String) The environment this cluster is being created in, for example, cloud.yugabyte.com

### Optional

- `use_secure_host` (Boolean) Set to true to use a secure connection (HTTPS) to the host.

