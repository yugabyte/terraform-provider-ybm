---
page_title: "Imports with generated config"
description: Using terraform generate-config-out to import resources
---


# Requirements

* Terraform >= 1.5.0

# Importing resources using terraform generate-config-out

Since 1.5.0 terraform introduce a new option to import and generates `.tf` files.

1. Set imports into your `.tf` files

```terraform
// Import VPC with id "000000-1111-41c1-9752-c0ad2fc9a6c0" into resource ybm_vpc.japan
// Do not create the resource in your tf file, terraform plan -generate-config-out will generated it.
import {
  to = ybm_vpc.japan
  id = "000000-1111-41c1-9752-c0ad2fc9a6c0"
}
```

2. Run the plan command with generate-config-out option

```shell
terraform plan -generate-config-out=generated_resources.tf
```

A new file generated_resources.tf should be created with the VPC resources

```terraform
# __generated__ by Terraform
# Please review these resources and move them into your main configuration files.

# __generated__ by Terraform from "000000-1111-41c1-9752-c0ad2fc9a6c0"
resource "ybm_vpc" "japan" {
  cloud       = "GCP"
  global_cidr = null
  name        = "example-vpc2"
  region_cidr_info = [
    {
      cidr   = "10.231.0.0/24"
      region = "europe-central2"
    },
    {
      cidr   = "10.9.0.0/24"
      region = "us-west2"
    },
  ]
  vpc_id = "000000-1111-41c1-9752-c0ad2fc9a6c0"
}
```

3. Run apply to finalize the import

```shell
terraform apply
```