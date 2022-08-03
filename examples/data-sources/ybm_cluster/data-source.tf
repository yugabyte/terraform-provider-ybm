variable "account_id" {
  type        = string
  description = "The account ID."
}

data "ybm_cluster" "clustername"{

  cluster_name= "example-terraform-cluster"
  account_id= var.account_id
} 