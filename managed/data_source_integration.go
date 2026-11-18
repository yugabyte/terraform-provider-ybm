/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	//"github.com/hashicorp/terraform-plugin-log/tflog"

	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *dataSourceIntegration) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The data source to fetch Yugabyte Aeon Integration",
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this integration belongs to.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this integration belongs to.",
				Computed:    true,
			},
			"config_id": schema.StringAttribute{
				Description: "The ID of the integration.",
				Computed:    true,
			},
			"config_name": schema.StringAttribute{
				Description: "The name of the integration",
				Required:    true,
			},
			"type": schema.StringAttribute{
				Description: "Defines different exporter destination types.",
				Computed:    true,
			},
			"is_valid": schema.BoolAttribute{
				Description: "Signifies whether the integration configuration is valid or not",
				Computed:    true,
			},
			"datadog_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a Datadog integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"api_key": schema.StringAttribute{
						Description: "Datadog Api Key",
						Computed:    true,
						Sensitive:   true,
					},
					"site": schema.StringAttribute{
						Description: "Datadog site.",
						Computed:    true,
					},
				},
			},
			"prometheus_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a Prometheus integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"endpoint": schema.StringAttribute{
						Description: "Prometheus OTLP endpoint URL e.g. http://my-prometheus-endpoint/api/v1/otlp",
						Computed:    true,
					},
				},
			},
			"victoriametrics_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a VictoriaMetrics integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"endpoint": schema.StringAttribute{
						Description: "VictoriaMetrics OTLP endpoint URL e.g. http://my-victoria-metrics-endpoint/opentelemetry",
						Computed:    true,
					},
				},
			},
			"grafana_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a Grafana integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"access_policy_token": schema.StringAttribute{
						Description: "Grafana Access Policy Token",
						Computed:    true,
						Sensitive:   true,
					},
					"zone": schema.StringAttribute{
						Description: "Grafana Zone.",
						Computed:    true,
					},
					"instance_id": schema.StringAttribute{
						Description: "Grafana InstanceID.",
						Computed:    true,
					},
					"org_slug": schema.StringAttribute{
						Description: "Grafana OrgSlug.",
						Computed:    true,
					},
				},
			},
			"sumologic_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a Sumo Logic integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"access_id": schema.StringAttribute{
						Description: "Sumo Logic Access Key ID",
						Computed:    true,
						Sensitive:   true,
					},
					"access_key": schema.StringAttribute{
						Description: "Sumo Logic Access Key",
						Computed:    true,
						Sensitive:   true,
					},
					"installation_token": schema.StringAttribute{
						Description: "A Sumo Logic installation token to export telemetry to Grafana with",
						Computed:    true,
						Sensitive:   true,
					},
				},
			},
			"googlecloud_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a Google Cloud integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Description: "Service Account Type",
						Computed:    true,
					},
					"project_id": schema.StringAttribute{
						Description: "GCP Project ID",
						Computed:    true,
					},
					"private_key": schema.StringAttribute{
						Description: "Private Key",
						Computed:    true,
					},
					"private_key_id": schema.StringAttribute{
						Description: "Private Key ID",
						Computed:    true,
					},
					"client_email": schema.StringAttribute{
						Description: "Client Email",
						Computed:    true,
					},
					"client_id": schema.StringAttribute{
						Description: "Client ID",
						Computed:    true,
					},
					"auth_uri": schema.StringAttribute{
						Description: "Auth URI",
						Computed:    true,
					},
					"token_uri": schema.StringAttribute{
						Description: "Token URI",
						Computed:    true,
					},
					"auth_provider_x509_cert_url": schema.StringAttribute{
						Description: "Auth Provider X509 Cert URL",
						Computed:    true,
					},
					"client_x509_cert_url": schema.StringAttribute{
						Description: "Client X509 Cert URL",
						Computed:    true,
					},
					"universe_domain": schema.StringAttribute{
						Description: "Google Universe Domain",
						Computed:    true,
					},
				},
			},
			"newrelic_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a Newrelic integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"endpoint": schema.StringAttribute{
						Description: "Newrelic Endpoint URL",
						Required:    true,
					},
					"license_key": schema.StringAttribute{
						Description: "Newrelic License Key",
						Required:    true,
						Sensitive:   true,
					},
				},
			},
			"aws_s3_spec": schema.SingleNestedAttribute{
				Description: "The specifications of an AWS S3 integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"bucket_name": schema.StringAttribute{
						Description: "The S3 bucket name to export logs to",
						Computed:    true,
					},
					"region": schema.StringAttribute{
						Description: "AWS region where the S3 bucket is located",
						Computed:    true,
					},
					"access_key_id": schema.StringAttribute{
						Description: "AWS Access Key ID for S3 access",
						Computed:    true,
						Sensitive:   true,
					},
					"secret_access_key": schema.StringAttribute{
						Description: "AWS Secret Access Key for S3 access. Use the Access Key ID and Secret Access Key of an IAM user with `s3:PutObject` permission for the S3 bucket. For more information, refer to [Managing access keys for IAM users](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_access-keys.html) in the AWS documentation.",
						Computed:    true,
						Sensitive:   true,
					},
					"path_prefix": schema.StringAttribute{
						Description: "S3 path prefix for organizing objects. Exported objects use the `{path_prefix}/{cluster-id}/{node-name}/{partition}/` structure, for example `yugabyte-logs/cluster-123/node-1/2024/01/07/14/`.",
						Computed:    true,
					},
					"file_prefix": schema.StringAttribute{
						Description: "Prefix for exported file names",
						Computed:    true,
					},
					"partition_strategy": schema.StringAttribute{
						Description: "Time-based partitioning: 'minute' or 'hour'",
						Computed:    true,
					},
				},
			},
			"tsuga_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a Tsuga integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"endpoint": schema.StringAttribute{
						Description: "Tsuga OTLP endpoint URL",
						Computed:    true,
					},
					"api_token": schema.StringAttribute{
						Description: "Tsuga bearer token (masked)",
						Computed:    true,
						Sensitive:   true,
					},
				},
			},
			"mimir_spec": schema.SingleNestedAttribute{
				Description: "The specifications of a Mimir integration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"endpoint": schema.StringAttribute{
						Description: "Mimir OTLP endpoint URL",
						Computed:    true,
					},
					"api_token": schema.StringAttribute{
						Description: "Mimir Authorization header value (masked)",
						Computed:    true,
						Sensitive:   true,
					},
					"org_id": schema.StringAttribute{
						Description: "Mimir tenant organization ID, sent as the X-Scope-OrgID header",
						Computed:    true,
					},
				},
			},
		},
	}
}

func newDataSourceIntegration() datasource.DataSource {
	return &dataSourceIntegration{}
}

func (r *dataSourceIntegration) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration"
}

func (r *dataSourceIntegration) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type dataSourceIntegration struct {
	p provider
}

// Read Integration data source
func (r dataSourceIntegration) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var tpConfig TelemetryProvider

	diags := resp.State.Set(ctx, &tpConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get the project ID ", message)
		return
	}

	telemetryProvider, readOK, message := dataSourceTelemetryProviderRead(accountId, projectId, tpConfig.ConfigName.ValueString(), apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the integration", message)
		return
	}

	diags = resp.State.Set(ctx, &telemetryProvider)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func GetTelemetryProviderByName(accountId string, projectId string, configName string, apiClient *openapiclient.APIClient) (*openapiclient.TelemetryProviderData, error) {
	resp, _, err := apiClient.TelemetryProviderApi.ListTelemetryProviders(context.Background(), accountId, projectId).Execute()

	if err != nil {
		return nil, err
	}

	for _, telemetryProvider := range resp.Data {
		if telemetryProvider.GetSpec().Name == configName {
			return &telemetryProvider, nil
		}
	}

	return nil, fmt.Errorf("could not find integration with name: '%s'", configName)
}

func dataSourceTelemetryProviderRead(accountId string, projectId string, configName string, apiClient *openapiclient.APIClient) (tp TelemetryProvider, readOK bool, errorMessage string) {

	configData, err := GetTelemetryProviderByName(accountId, projectId, configName, apiClient)
	if err != nil {
		return tp, false, GetApiErrorDetails(err)
	}

	configSpec := configData.GetSpec()
	configInfo := configData.GetInfo()

	tp.AccountID = types.StringValue(accountId)
	tp.ProjectID = types.StringValue(projectId)
	tp.ConfigName = types.StringValue(configSpec.Name)
	tp.ConfigID = types.StringValue(configInfo.Id)
	tp.Type = types.StringValue(string(configSpec.Type))
	tp.IsValid = types.BoolValue(*configInfo.IsValid.Get())

	switch configSpec.Type {
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG:
		tp.DataDogSpec = &DataDogSpec{
			ApiKey: types.StringValue(configSpec.DatadogSpec.Get().ApiKey),
			Site:   types.StringValue(configSpec.DatadogSpec.Get().Site),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_PROMETHEUS:
		tp.PrometheusSpec = &PrometheusSpec{
			Endpoint: types.StringValue(configSpec.PrometheusSpec.Get().Endpoint),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_VICTORIAMETRICS:
		tp.VictoriaMetricsSpec = &VictoriaMetricsSpec{
			Endpoint: types.StringValue(configSpec.VictoriametricsSpec.Get().Endpoint),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		grafanaSpec := configSpec.GetGrafanaSpec()
		tp.GrafanaSpec = &GrafanaSpec{
			AccessTokenPolicy: types.StringValue(grafanaSpec.AccessPolicyToken),
			Zone:              types.StringValue(grafanaSpec.Zone),
			InstanceId:        types.StringValue(grafanaSpec.InstanceId),
			OrgSlug:           types.StringValue(grafanaSpec.OrgSlug),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		sumoLogicSpec := configSpec.GetSumologicSpec()
		tp.SumoLogicSpec = &SumoLogicSpec{
			AccessKey:         types.StringValue(sumoLogicSpec.AccessKey),
			AccessId:          types.StringValue(sumoLogicSpec.AccessId),
			InstallationToken: types.StringValue(sumoLogicSpec.InstallationToken),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD:
		googlecloudSpec := configSpec.GetGooglecloudSpec()
		tp.GoogleCloudSpec = &GCPServiceAccount{
			Type:                    types.StringValue(googlecloudSpec.Type),
			ProjectId:               types.StringValue(googlecloudSpec.ProjectId),
			PrivateKeyId:            types.StringValue(googlecloudSpec.PrivateKeyId),
			PrivateKey:              types.StringValue(googlecloudSpec.PrivateKey),
			ClientEmail:             types.StringValue(googlecloudSpec.ClientEmail),
			ClientId:                types.StringValue(googlecloudSpec.ClientId),
			AuthUri:                 types.StringValue(googlecloudSpec.AuthUri),
			TokenUri:                types.StringValue(googlecloudSpec.TokenUri),
			AuthProviderX509CertUrl: types.StringValue(googlecloudSpec.AuthProviderX509CertUrl),
			ClientX509CertUrl:       types.StringValue(googlecloudSpec.ClientX509CertUrl),
			UniverseDomain:          types.StringValue(googlecloudSpec.GetUniverseDomain()),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_NEWRELIC:
		newRelicSpec := configSpec.GetNewrelicSpec()
		tp.NewRelicSpec = &NewRelicSpec{
			Endpoint:   types.StringValue(newRelicSpec.Endpoint),
			LicenseKey: types.StringValue(newRelicSpec.LicenseKey),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_AWS_S3:
		s3Spec := configSpec.GetAwsS3Spec()
		tp.AwsS3Spec = &AwsS3Spec{
			BucketName:      types.StringValue(s3Spec.GetBucketName()),
			Region:          types.StringValue(s3Spec.GetRegion()),
			AccessKeyId:     types.StringValue(s3Spec.GetAccessKeyId()),
			SecretAccessKey: types.StringValue(s3Spec.GetSecretAccessKey()),
			PathPrefix:      types.StringValue(s3Spec.GetPathPrefix()),
			// Optional fields; "" when the API response does not have them.
			FilePrefix:        types.StringValue(s3Spec.GetFilePrefix()),
			PartitionStrategy: types.StringValue(s3Spec.GetPartitionStrategy()),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_TSUGA:
		tsugaSpec := configSpec.GetTsugaSpec()
		tp.TsugaSpec = &TsugaSpec{
			Endpoint: types.StringValue(tsugaSpec.Endpoint),
			ApiToken: types.StringValue(tsugaSpec.ApiToken),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_MIMIR:
		mimirSpec := configSpec.GetMimirSpec()
		tp.MimirSpec = &MimirSpec{
			Endpoint: types.StringValue(mimirSpec.Endpoint),
			ApiToken: types.StringValue(mimirSpec.ApiToken),
			OrgId:    types.StringNull(),
		}
		if mimirSpec.GetOrgId() != "" {
			tp.MimirSpec.OrgId = types.StringValue(mimirSpec.GetOrgId())
		}
	}

	return tp, true, ""
}
