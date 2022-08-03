variable "account_id" {
  type        = string
  description = "The account ID."
}

data "ybm_cluster" "clustername"{

  cluster_name= "terraform-test-posriniv-1"
  account_id= var.account_id
} 