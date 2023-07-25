#Create Private service endpoint (Private link in AWS).
resource "ybm_private_service_endpoint" "my_pse" {
  cluster_id          = "00000-ca0a-1111-2222-3cc19ac7fab3"
  region              = "ap-northeast-1"
  security_principals = ["arn:aws:iam::111111111:root"]
}
