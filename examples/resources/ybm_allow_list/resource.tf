variable "account_id" {
  type        = string
  description = "The account ID."
}

resource "ybm_allow_list" "example_allow_list" {
  account_id = var.account_id
  allow_list_name = "allow-all"
  allow_list_description = "allow all the ip addresses"
  cidr_list = ["0.0.0.0/0"]  
}