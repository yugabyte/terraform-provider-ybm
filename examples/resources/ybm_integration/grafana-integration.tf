resource "ybm_integration" "grafana" {
  config_name = "grafana-example"
  type        = "GRAFANA"
  grafana_spec = {
    access_policy_token = "<access-policy-token>"
    instance_id         = "<instance-id>"
    org_slug            = "<org-slug>"
    zone                = "<zone>"
  }
}
