resource "ybm_integration" "victoriametrics" {
  config_name = "victoriametrics-example"
  type        = "VICTORIAMETRICS"
  victoriametrics_spec = {
    endpoint = "http://my-victoria-metrics-endpoint/opentelemetry"
  }
}
