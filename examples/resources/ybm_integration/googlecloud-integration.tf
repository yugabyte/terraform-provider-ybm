resource "ybm_integration" "gcp" {
  config_name = "gcp-example"
  type        = "GOOGLECLOUD"
  googlecloud_spec = {
    type                        = "service_account"
    project_id                  = "<project_id>"
    private_key_id              = "<private_key_id>"
    private_key                 = "<private_key>"
    client_email                = "<client_email>"
    client_id                   = "<client_id>"
    auth_uri                    = "<auth_uri>"
    token_uri                   = "<token_uri>"
    auth_provider_x509_cert_url = "<auth_provider_x509_cert_url>"
    client_x509_cert_url        = "<client_x509_cert_url>"
    universe_domain             = "<universe_domain>"
  }
}