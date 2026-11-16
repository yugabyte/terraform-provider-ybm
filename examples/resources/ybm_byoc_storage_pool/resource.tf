resource "ybm_byoc_storage_pool" "payments" {
  environment_id = ybm_byoc_environment.example.environment_id
  name           = "payments"

  # Install the environment's agent first; see the ybm_byoc_environment example
  depends_on = [helm_release.agent]
}
