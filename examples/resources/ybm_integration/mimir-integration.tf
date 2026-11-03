resource "ybm_integration" "mimir" {
  config_name = "mimir-example"
  type        = "MIMIR"
  mimir_spec = {
    endpoint  = "<mimir otlp endpoint url>" # e.g. https://mimir.my-company.com/otlp
    api_token = "Bearer <mimir token>"      # full Authorization header value, must start with "Bearer " or "Basic "
    org_id    = "<mimir tenant org id>"     # Optional, sent as the X-Scope-OrgID header
  }
}
