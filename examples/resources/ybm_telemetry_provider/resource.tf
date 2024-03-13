# Example for Datadog
resource "ybm_telemetry_provider" "datadog" {
  config_name = "datadog-example"
  type        = "DATADOG"
  datadog_spec = {
    api_key = "<api-key>"
    site    = "datadoghq.com"
  }
}

# Example for Grafana
resource "ybm_telemetry_provider" "grafana" {
  config_name = "grafana-example"
  type        = "GRAFANA"
  grafana_spec = {
    access_policy_token = "<access-policy-token>"
    instance_id         = "<instance-id>"
    org_slug            = "<org-slug>"
    zone                = "<zone>"
  }
}

# Example for Sumologic
resource "ybm_telemetry_provider" "sumologic" {
  config_name = "sumologic-example"
  type        = "SUMOLOGIC"
  sumologic_spec = {
    access_id          = "<access-id>"
    access_key         = "<access-key>"
    installation_token = "<installation-token>"
  }
}