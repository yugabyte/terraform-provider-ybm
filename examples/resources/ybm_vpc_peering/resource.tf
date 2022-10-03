#AWS VPC Peering

resource "ybm_vpc_peering" "example_vpc_peering" {
  name = "example_name"
  yugabytedb_vpc_id = "example_vpc_id"
  application_vpc_info = {
    cloud = "AWS"
    account_id = "example_account_id"
    region = "us-west1"
    vpc_id = "application_vpc_id"
    cidr = "example_cidr"
  }
}

#GCP VPC Peering
resource "ybm_vpc_peering" "example_vpc_peering" {
  name = "example_name"
  yugabytedb_vpc_id = "example_vpc_id"
  application_vpc_info = {
    cloud = "GCP"
    project = "example_project"
    vpc_id = "application_vpc_id"
  }
}