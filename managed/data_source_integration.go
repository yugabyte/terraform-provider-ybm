/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	//"github.com/hashicorp/terraform-plugin-log/tflog"

	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type dataSourceIntegrationType struct{}

func (r dataSourceIntegrationType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "The data source to fetch Yugabyte Aeon Integration",
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this integration belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this integration belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_id": {
				Description: "The ID of the integration.",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_name": {
				Description: "The name of the integration",
				Type:        types.StringType,
				Required:    true,
			},
			"type": {
				Description: "Defines different exporter destination types.",
				Type:        types.StringType,
				Computed:    true,
			},
			"is_valid": {
				Description: "Signifies whether the integration configuration is valid or not",
				Type:        types.BoolType,
				Computed:    true,
			},
			"datadog_spec": {
				Description: "The specifications of a Datadog integration.",
				Computed:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"api_key": {
						Description: "Datadog Api Key",
						Type:        types.StringType,
						Computed:    true,
						Sensitive:   true,
					},
					"site": {
						Description: "Datadog site.",
						Type:        types.StringType,
						Computed:    true,
					},
				}),
			},
			"grafana_spec": {
				Description: "The specifications of a Grafana integration.",
				Computed:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"access_policy_token": {
						Description: "Grafana Access Policy Token",
						Type:        types.StringType,
						Computed:    true,
						Sensitive:   true,
					},
					"zone": {
						Description: "Grafana Zone.",
						Type:        types.StringType,
						Computed:    true,
					},
					"instance_id": {
						Description: "Grafana InstanceID.",
						Type:        types.StringType,
						Computed:    true,
					},
					"org_slug": {
						Description: "Grafana OrgSlug.",
						Type:        types.StringType,
						Computed:    true,
					},
				}),
			},
			"sumologic_spec": {
				Description: "The specifications of a Sumo Logic integration.",
				Computed:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"access_id": {
						Description: "Sumo Logic Access Key ID",
						Type:        types.StringType,
						Computed:    true,
						Sensitive:   true,
					},
					"access_key": {
						Description: "Sumo Logic Access Key",
						Type:        types.StringType,
						Computed:    true,
						Sensitive:   true,
					},
					"installation_token": {
						Description: "A Sumo Logic installation token to export telemetry to Grafana with",
						Type:        types.StringType,
						Computed:    true,
						Sensitive:   true,
					},
				}),
			},
			"googlecloud_spec": {
				Description: "The specifications of a Google Cloud integration.",
				Computed:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"type": {
						Description: "Service Account Type",
						Type:        types.StringType,
						Computed:    true,
					},
					"project_id": {
						Description: "GCP Project ID",
						Type:        types.StringType,
						Computed:    true,
					},
					"private_key": {
						Description: "Private Key",
						Type:        types.StringType,
						Computed:    true,
					},
					"private_key_id": {
						Description: "Private Key ID",
						Type:        types.StringType,
						Computed:    true,
					},
					"client_email": {
						Description: "Client Email",
						Type:        types.StringType,
						Computed:    true,
					},
					"client_id": {
						Description: "Client ID",
						Type:        types.StringType,
						Computed:    true,
					},
					"auth_uri": {
						Description: "Auth URI",
						Type:        types.StringType,
						Computed:    true,
					},
					"token_uri": {
						Description: "Token URI",
						Type:        types.StringType,
						Computed:    true,
					},
					"auth_provider_x509_cert_url": {
						Description: "Auth Provider X509 Cert URL",
						Type:        types.StringType,
						Computed:    true,
					},
					"client_x509_cert_url": {
						Description: "Client X509 Cert URL",
						Type:        types.StringType,
						Computed:    true,
					},
					"universe_domain": {
						Description: "Google Universe Domain",
						Type:        types.StringType,
						Computed:    true,
					},
				}),
			},
		},
	}, nil
}

func (r dataSourceIntegrationType) NewDataSource(_ context.Context, p tfsdk.Provider) (tfsdk.DataSource, diag.Diagnostics) {
	return dataSourceIntegration{
		p: *(p.(*provider)),
	}, nil
}

type dataSourceIntegration struct {
	p provider
}

// Read Integration data source
func (r dataSourceIntegration) Read(ctx context.Context, req tfsdk.ReadDataSourceRequest, resp *tfsdk.ReadDataSourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var tpConfig TelemetryProvider

	diags := req.Config.Get(ctx, &tpConfig)
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

	telemetryProvider, readOK, message := dataSourceTelemetryProviderRead(accountId, projectId, tpConfig.ConfigName.Value, apiClient)
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

	tp.AccountID.Value = accountId
	tp.ProjectID.Value = projectId
	tp.ConfigName.Value = configSpec.Name
	tp.ConfigID.Value = configInfo.Id
	tp.Type.Value = string(configSpec.Type)
	tp.IsValid.Value = *configInfo.IsValid.Get()

	switch configSpec.Type {
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG:
		tp.DataDogSpec = &DataDogSpec{
			ApiKey: types.String{Value: configSpec.DatadogSpec.Get().ApiKey},
			Site:   types.String{Value: configSpec.DatadogSpec.Get().Site},
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		grafanaSpec := configSpec.GetGrafanaSpec()
		tp.GrafanaSpec = &GrafanaSpec{
			AccessTokenPolicy: types.String{Value: grafanaSpec.AccessPolicyToken},
			Zone:              types.String{Value: grafanaSpec.Zone},
			InstanceId:        types.String{Value: grafanaSpec.InstanceId},
			OrgSlug:           types.String{Value: grafanaSpec.OrgSlug},
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		sumoLogicSpec := configSpec.GetSumologicSpec()
		tp.SumoLogicSpec = &SumoLogicSpec{
			AccessKey:         types.String{Value: sumoLogicSpec.AccessKey},
			AccessId:          types.String{Value: sumoLogicSpec.AccessId},
			InstallationToken: types.String{Value: sumoLogicSpec.InstallationToken},
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD:
		googlecloudSpec := configSpec.GetGooglecloudSpec()
		tp.GoogleCloudSpec = &GCPServiceAccount{
			Type:                    types.String{Value: googlecloudSpec.Type},
			ProjectId:               types.String{Value: googlecloudSpec.ProjectId},
			PrivateKeyId:            types.String{Value: googlecloudSpec.PrivateKeyId},
			PrivateKey:              types.String{Value: googlecloudSpec.PrivateKey},
			ClientEmail:             types.String{Value: googlecloudSpec.ClientEmail},
			ClientId:                types.String{Value: googlecloudSpec.ClientId},
			AuthUri:                 types.String{Value: googlecloudSpec.AuthUri},
			TokenUri:                types.String{Value: googlecloudSpec.TokenUri},
			AuthProviderX509CertUrl: types.String{Value: googlecloudSpec.AuthProviderX509CertUrl},
			ClientX509CertUrl:       types.String{Value: googlecloudSpec.ClientX509CertUrl},
		}
		if googlecloudSpec.HasUniverseDomain() {
			tp.GoogleCloudSpec.UniverseDomain = types.String{Value: *googlecloudSpec.UniverseDomain}
		}
	}

	return tp, true, ""
}
