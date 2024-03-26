resource "ybm_integration" "datadog" {
  config_name = "datadog-example"
  type        = "DATADOG"
  datadog_spec = {
    api_key = "<api-key>"
    site    = "datadoghq.com"
  }
}
