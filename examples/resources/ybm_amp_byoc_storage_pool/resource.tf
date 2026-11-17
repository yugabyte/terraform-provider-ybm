resource "ybm_amp_byoc_storage_pool" "payments" {
  environment_id = ybm_amp_byoc_environment.example.environment_id
  name           = "payments"

  # Install the environment's agent first; see the ybm_amp_byoc_environment example
  depends_on = [helm_release.agent]
}
