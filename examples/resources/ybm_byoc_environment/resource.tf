resource "ybm_byoc_environment" "example" {
  name   = "acme-prod"
  region = "us-east-1"
}

# Install the agent in your Kubernetes cluster with this command; the environment
# becomes READY once the agent enrolls with the API key.
output "agent_install_hint" {
  value = ybm_byoc_environment.example.install_hint
}

output "agent_api_key" {
  value     = ybm_byoc_environment.example.agent_api_key
  sensitive = true
}
