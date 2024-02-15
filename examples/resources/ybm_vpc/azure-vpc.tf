resource "ybm_vpc" "example-vpc" {
  name  = "example-vpc"
  cloud = "AZURE"
  region_cidr_info = [
    {
      region = "eastus"
      # For Azure, the CIDR is auto-assigned
      # cidr = "10.231.0.0/24"
    }
  ]
}