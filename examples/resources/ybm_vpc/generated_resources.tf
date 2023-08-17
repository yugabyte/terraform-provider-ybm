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
