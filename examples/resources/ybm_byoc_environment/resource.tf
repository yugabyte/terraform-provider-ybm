resource "ybm_byoc_environment" "example" {
  name = "acme-prod"

  # Optional: where the environment runs, passed to the agent by the install command
  placement = "cloud"
}

# The environment, its agent and its clusters can come from one apply: the agent's
# install reads the API key from a secret, and clusters wait for the agent to enroll.
# The key is only returned by the apply that registers the environment, so keep it
# in the secret; importing the environment cannot recover it, and replacing the
# environment revokes it.
resource "kubernetes_namespace" "agent" {
  metadata {
    name = "ycp-agent"
  }
}

resource "kubernetes_secret" "agent_api_key" {
  metadata {
    name      = "amp-agent-api-key"
    namespace = kubernetes_namespace.agent.metadata[0].name
  }
  data = {
    "api-key" = ybm_byoc_environment.example.agent_api_key
  }
}

# install_hint lists every value the agent's chart needs, such as the image tag and
# the database it stores its state in; put them in agent-values.yaml.
resource "helm_release" "agent" {
  name      = "yb-amp-controller"
  chart     = "oci://quay.io/yugabyte/charts/yb-amp-controller"
  namespace = kubernetes_namespace.agent.metadata[0].name
  timeout   = 600
  values    = [file("${path.module}/agent-values.yaml")]

  set {
    name  = "agent.enabled"
    value = "true"
  }
  set {
    name  = "agent.ycpUrl"
    value = var.ycp_url # the agent.ycpUrl in install_hint
  }
  set {
    name  = "agent.envName"
    value = ybm_byoc_environment.example.environment_id
  }
  set {
    name  = "agent.placement"
    value = ybm_byoc_environment.example.placement
  }
  set {
    name  = "agent.apiKeySecret.name"
    value = kubernetes_secret.agent_api_key.metadata[0].name
  }
}

resource "ybm_byoc_storage_pool" "payments" {
  environment_id = ybm_byoc_environment.example.environment_id
  name           = "payments"

  # A failed install fails here at once rather than after the wait for the agent
  depends_on = [helm_release.agent]
}

output "agent_install_hint" {
  value = ybm_byoc_environment.example.install_hint
}
