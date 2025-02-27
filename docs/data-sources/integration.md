---
page_title: "ybm_integration Data Source - YugabyteDB Aeon"
description: |-
  The data source to fetch Yugabyte Aeon Integration
---

# ybm_integration (Data Source)

The data source to fetch Yugabyte Aeon Integration


## Example Usage

```terraform
data "ybm_integration" "example_name" {
  config_name = "name-of-integration"
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `config_name` (String) The name of the integration

### Read-Only

- `account_id` (String) The ID of the account this integration belongs to.
- `config_id` (String) The ID of the integration.
- `datadog_spec` (Attributes) The specifications of a Datadog integration. (see [below for nested schema](#nestedatt--datadog_spec))
- `googlecloud_spec` (Attributes) The specifications of a Google Cloud integration. (see [below for nested schema](#nestedatt--googlecloud_spec))
- `grafana_spec` (Attributes) The specifications of a Grafana integration. (see [below for nested schema](#nestedatt--grafana_spec))
- `is_valid` (Boolean) Signifies whether the integration configuration is valid or not
- `project_id` (String) The ID of the project this integration belongs to.
- `prometheus_spec` (Attributes) The specifications of a Prometheus integration. (see [below for nested schema](#nestedatt--prometheus_spec))
- `sumologic_spec` (Attributes) The specifications of a Sumo Logic integration. (see [below for nested schema](#nestedatt--sumologic_spec))
- `type` (String) Defines different exporter destination types.
- `victoriametrics_spec` (Attributes) The specifications of a VictoriaMetrics integration. (see [below for nested schema](#nestedatt--victoriametrics_spec))

<a id="nestedatt--datadog_spec"></a>
### Nested Schema for `datadog_spec`

Read-Only:

- `api_key` (String, Sensitive) Datadog Api Key
- `site` (String) Datadog site.


<a id="nestedatt--googlecloud_spec"></a>
### Nested Schema for `googlecloud_spec`

Read-Only:

- `auth_provider_x509_cert_url` (String) Auth Provider X509 Cert URL
- `auth_uri` (String) Auth URI
- `client_email` (String) Client Email
- `client_id` (String) Client ID
- `client_x509_cert_url` (String) Client X509 Cert URL
- `private_key` (String) Private Key
- `private_key_id` (String) Private Key ID
- `project_id` (String) GCP Project ID
- `token_uri` (String) Token URI
- `type` (String) Service Account Type
- `universe_domain` (String) Google Universe Domain


<a id="nestedatt--grafana_spec"></a>
### Nested Schema for `grafana_spec`

Read-Only:

- `access_policy_token` (String, Sensitive) Grafana Access Policy Token
- `instance_id` (String) Grafana InstanceID.
- `org_slug` (String) Grafana OrgSlug.
- `zone` (String) Grafana Zone.


<a id="nestedatt--prometheus_spec"></a>
### Nested Schema for `prometheus_spec`

Read-Only:

- `endpoint` (String) Prometheus OTLP endpoint URL e.g. http://my-prometheus-endpoint/api/v1/otlp


<a id="nestedatt--sumologic_spec"></a>
### Nested Schema for `sumologic_spec`

Read-Only:

- `access_id` (String, Sensitive) Sumo Logic Access Key ID
- `access_key` (String, Sensitive) Sumo Logic Access Key
- `installation_token` (String, Sensitive) A Sumo Logic installation token to export telemetry to Grafana with


<a id="nestedatt--victoriametrics_spec"></a>
### Nested Schema for `victoriametrics_spec`

Read-Only:

- `endpoint` (String) VictoriaMetrics OTLP endpoint URL e.g. http://my-victoria-metrics-endpoint/opentelemetry
