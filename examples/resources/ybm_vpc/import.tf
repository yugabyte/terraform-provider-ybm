// Import VPC with id "000000-1111-41c1-9752-c0ad2fc9a6c0" into resource ybm_vpc.japan
// Do not create the resource in your tf file, terraform plan -generate-config-out will generated it.
import {
  to = ybm_vpc.japan
  id = "000000-1111-41c1-9752-c0ad2fc9a6c0"
}
