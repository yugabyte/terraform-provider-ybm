resource "ybm_vpc_peering" "example_vpc_peering" {
  account_id = var.account_id
  name = "example_name"
  yugabytedb_vpc_id = "example_vpc_id"
  application_vpc_info = {
    cloud_type = "GCP"
    cloud_project = "example_project"
    cloud_region = "us-west1"
    vpc_id = "application_vpc_id"
    cidr = "example_cidr"
  }
}