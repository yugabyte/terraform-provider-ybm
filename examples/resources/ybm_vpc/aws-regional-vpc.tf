resource "ybm_vpc" "example-vpc" {
  name = "example-vpc"
  cloud = "AWS"
  region_cidr_info = [
    {
      region = "us-east-1"
      cidr = "10.231.0.0/24"
    }
  ]
}