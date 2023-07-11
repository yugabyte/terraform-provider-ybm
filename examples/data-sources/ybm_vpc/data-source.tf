
# Using VPC name as parameter
data "ybm_vpc" "example_vpc" {
  name = "my-vpc"
}

# Using VPC id as parameter
data "ybm_vpc" "example_vpc" {
  id = "55555555-6666-4444-3333-111111111111"
}
