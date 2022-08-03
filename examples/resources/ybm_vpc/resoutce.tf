resource "ybm_vpc" "example-vpc" {
  account_id = "example-account-id"
  name = "example-vpc"
  cloud = "GCP"
  # Use only one among global cidr and region cidr
  global_cidr = "10.9.0.0/18"
  # region_cidr_info = [
  #   {
  #     region = "europe-central2"
  #     cidr = "10.231.0.0/24"
  #   },
  #   {
  #     region = "us-west2" 
  #     cidr = "10.9.0.0/24"
  #   }
  # ]
}