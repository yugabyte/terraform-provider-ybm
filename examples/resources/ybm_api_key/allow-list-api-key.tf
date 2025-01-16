resource "ybm_allow_list" "external_network_range" {
  allow_list_name        = "external-range"
  allow_list_description = "allow a range of external IP addresses"
  cidr_list              = ["192.168.1.0/24"]
}
resource "ybm_allow_list" "external_single_ip" {
  allow_list_name        = "external-single"
  allow_list_description = "allow a single external IP address"
  cidr_list              = ["203.0.113.1/32"]
}

resource "ybm_api_key" "developer_api_key" {
  name           = "developer-key"
  description    = "IP restricted API key for developer access"
  duration       = 1
  unit           = "Hours"
  role_name      = "Developer"
  allow_list_ids = [ybm_allow_list.external_network_range.allow_list_id, ybm_allow_list.external_single_ip.allow_list_id]
}
