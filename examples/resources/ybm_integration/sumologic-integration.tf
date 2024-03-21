resource "ybm_integration" "sumologic" {
  config_name = "sumologic-example"
  type        = "SUMOLOGIC"
  sumologic_spec = {
    access_id          = "<access-id>"
    access_key         = "<access-key>"
    installation_token = "<installation-token>"
  }
}
