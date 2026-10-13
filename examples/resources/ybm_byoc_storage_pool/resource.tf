resource "ybm_byoc_storage_pool" "payments" {
  environment_id = ybm_byoc_environment.example.environment_id
  name           = "payments"
}
