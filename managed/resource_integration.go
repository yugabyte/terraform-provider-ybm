/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/schemavalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/yugabyte/terraform-provider-ybm/managed/fflags"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceIntegrationType struct{}

func (r resourceIntegrationType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "The resource to create an integration in YugabyteDB Aeon.",
		Attributes:  r.getSchemaAttributes(),
	}, nil
}

func (r resourceIntegrationType) getSchemaAttributes() map[string]tfsdk.Attribute {
	return map[string]tfsdk.Attribute{
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
			Required:    true,
			Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("DATADOG", "GRAFANA", "SUMOLOGIC", "GOOGLECLOUD", "PROMETHEUS")},
		},
		"is_valid": {
			Description: "Signifies whether the integration configuration is valid or not",
			Type:        types.BoolType,
			Computed:    true,
		},
		"datadog_spec": {
			Description: "The specifications of a Datadog integration.",
			Optional:    true,
			Validators:  onlyContainsPath("datadog_spec"),
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"api_key": {
					Description: "Datadog Api Key",
					Type:        types.StringType,
					Required:    true,
					Sensitive:   true,
				},
				"site": {
					Description: "Datadog site.",
					Type:        types.StringType,
					Required:    true,
				},
			}),
		},
		"prometheus_spec": {
			Description: "The specifications of a Prometheus integration.",
			Optional:    true,
			Validators:  onlyContainsPath("prometheus_spec"),
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"endpoint": {
					Description: "Prometheus OTLP endpoint URL",
					Type:        types.StringType,
					Required:    true,
				},
			}),
		},
		"grafana_spec": {
			Description: "The specifications of a Grafana integration.",
			Optional:    true,
			Validators:  onlyContainsPath("grafana_spec"),
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"access_policy_token": {
					Description: "Grafana Access Policy Token",
					Type:        types.StringType,
					Required:    true,
					Sensitive:   true,
				},
				"zone": {
					Description: "Grafana Zone.",
					Type:        types.StringType,
					Required:    true,
				},
				"instance_id": {
					Description: "Grafana InstanceID.",
					Type:        types.StringType,
					Required:    true,
				},
				"org_slug": {
					Description: "Grafana OrgSlug.",
					Type:        types.StringType,
					Required:    true,
				},
			}),
		},
		"sumologic_spec": {
			Description: "The specifications of a Sumo Logic integration.",
			Optional:    true,
			Validators:  onlyContainsPath("sumologic_spec"),
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"access_id": {
					Description: "Sumo Logic Access Key ID",
					Type:        types.StringType,
					Required:    true,
					Sensitive:   true,
				},
				"access_key": {
					Description: "Sumo Logic Access Key",
					Type:        types.StringType,
					Required:    true,
					Sensitive:   true,
				},
				"installation_token": {
					Description: "A Sumo Logic installation token to export telemetry to Grafana with",
					Type:        types.StringType,
					Required:    true,
					Sensitive:   true,
				},
			}),
		},
		"googlecloud_spec": {
			Description: "The specifications of a Google Cloud integration.",
			Optional:    true,
			Validators:  onlyContainsPath("googlecloud_spec"),
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"type": {
					Description: "Service Account Type",
					Type:        types.StringType,
					Required:    true,
				},
				"project_id": {
					Description: "GCP Project ID",
					Type:        types.StringType,
					Required:    true,
				},
				"private_key": {
					Description: "Private Key",
					Type:        types.StringType,
					Required:    true,
					Sensitive:   true,
				},
				"private_key_id": {
					Description: "Private Key ID",
					Type:        types.StringType,
					Required:    true,
				},
				"client_email": {
					Description: "Client Email",
					Type:        types.StringType,
					Required:    true,
				},
				"client_id": {
					Description: "Client ID",
					Type:        types.StringType,
					Required:    true,
				},
				"auth_uri": {
					Description: "Auth URI",
					Type:        types.StringType,
					Required:    true,
				},
				"token_uri": {
					Description: "Token URI",
					Type:        types.StringType,
					Required:    true,
				},
				"auth_provider_x509_cert_url": {
					Description: "Auth Provider X509 Cert URL",
					Type:        types.StringType,
					Required:    true,
				},
				"client_x509_cert_url": {
					Description: "Client X509 Cert URL",
					Type:        types.StringType,
					Required:    true,
				},
				"universe_domain": {
					Description: "Google Universe Domain",
					Type:        types.StringType,
					Optional:    true,
				},
			}),
		},
	}
}

func onlyContainsPath(requiredPath string) []tfsdk.AttributeValidator {
	allPaths := []string{"datadog_spec", "grafana_spec", "sumologic_spec", "googlecloud_spec", "prometheus_spec"}
	var validators []tfsdk.AttributeValidator

	for _, specPath := range allPaths {
		if specPath != requiredPath {
			validators = append(validators, schemavalidator.ConflictsWith(path.MatchRoot(specPath)))
		}
	}

	return validators
}

func (r resourceIntegrationType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceIntegration{
		p: *(p.(*provider)),
	}, nil
}

type resourceIntegration struct {
	p provider
}

func getIntegrationPlan(ctx context.Context, plan tfsdk.Plan, tp *TelemetryProvider) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("config_name"), &tp.ConfigName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("type"), &tp.Type)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("datadog_spec"), &tp.DataDogSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("prometheus_spec"), &tp.PrometheusSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("grafana_spec"), &tp.GrafanaSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("sumologic_spec"), &tp.SumoLogicSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("googlecloud_spec"), &tp.GoogleCloudSpec)...)
	return diags
}

func getIDsFromIntegrationState(ctx context.Context, state tfsdk.State, tp *TelemetryProvider) {
	state.GetAttribute(ctx, path.Root("account_id"), &tp.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &tp.ProjectID)
	state.GetAttribute(ctx, path.Root("config_id"), &tp.ConfigID)
	state.GetAttribute(ctx, path.Root("type"), &tp.Type)
	switch tp.Type.Value {
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG):
		state.GetAttribute(ctx, path.Root("datadog_spec"), &tp.DataDogSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_PROMETHEUS):
		state.GetAttribute(ctx, path.Root("prometheus_spec"), &tp.PrometheusSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA):
		state.GetAttribute(ctx, path.Root("grafana_spec"), &tp.GrafanaSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC):
		state.GetAttribute(ctx, path.Root("sumologic_spec"), &tp.SumoLogicSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD):
		state.GetAttribute(ctx, path.Root("googlecloud_spec"), &tp.GoogleCloudSpec)
	}
}

func (r resourceIntegration) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan TelemetryProvider
	resp.Diagnostics.Append(getIntegrationPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the integration")
		return
	}

	sinkType := plan.Type.Value
	telemetrySinkTypeEnum, err := openapiclient.NewTelemetryProviderTypeEnumFromValue(strings.ToUpper(sinkType))
	if err != nil {
		resp.Diagnostics.AddError(GetApiErrorDetails(err), "")
		return
	}
	if *telemetrySinkTypeEnum == openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD && !fflags.IsFeatureFlagEnabled(fflags.GOOGLECLOUD_INTEGRATION_ENABLED) {
		resp.Diagnostics.AddError("Invalid integration type", "Integration of type GOOGLECLOUD is currently not supported")
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
		resp.Diagnostics.AddError("Unable to get the project ID", message)
		return
	}

	configName := plan.ConfigName.Value

	telemetryProviderSpec := openapiclient.NewTelemetryProviderSpec(configName, *telemetrySinkTypeEnum)

	switch *telemetrySinkTypeEnum {
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG:
		if plan.DataDogSpec == nil {
			resp.Diagnostics.AddError(
				"datadog_spec is required for type DATADOG",
				"datadog_spec is required when telemetry sink is DATADOG. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetDatadogSpec(*openapiclient.NewDatadogTelemetryProviderSpec(plan.DataDogSpec.ApiKey.Value, plan.DataDogSpec.Site.Value))
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_PROMETHEUS:
		if plan.PrometheusSpec == nil {
			resp.Diagnostics.AddError(
				"prometheus_spec is required for type PROMETHEUS",
				"prometheus_spec is required when telemetry sink is PROMETHEUS. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetPrometheusSpec(*openapiclient.NewPrometheusTelemetryProviderSpec(plan.PrometheusSpec.Endpoint.Value))
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		if plan.GrafanaSpec == nil {
			resp.Diagnostics.AddError(
				"grafana_spec is required for type GRAFANA",
				"grafana_spec is required when telemetry sink is GRAFANA. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetGrafanaSpec(*openapiclient.NewGrafanaTelemetryProviderSpec(plan.GrafanaSpec.AccessTokenPolicy.Value, plan.GrafanaSpec.Zone.Value, plan.GrafanaSpec.InstanceId.Value, plan.GrafanaSpec.OrgSlug.Value))

	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		if plan.SumoLogicSpec == nil {
			resp.Diagnostics.AddError(
				"sumologic_spec is required for type SUMOLOGIC",
				"sumologic_spec is required when telemetry sink is SUMOLOGIC. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetSumologicSpec(*openapiclient.NewSumologicTelemetryProviderSpec(plan.SumoLogicSpec.InstallationToken.Value, plan.SumoLogicSpec.AccessId.Value, plan.SumoLogicSpec.AccessKey.Value))

	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD:
		if plan.GoogleCloudSpec == nil {
			resp.Diagnostics.AddError(
				"googlecloud_spec is required for type GOOGLECLOUD",
				"googlecloud_spec is required when telemetry sink is GOOGLECLOUD. Please include this field in the resource",
			)
			return
		}
		gcpServiceAccountPlan := plan.GoogleCloudSpec
		googleCloudSpec := *openapiclient.NewGCPServiceAccount(gcpServiceAccountPlan.Type.Value, gcpServiceAccountPlan.ProjectId.Value, gcpServiceAccountPlan.PrivateKey.Value, gcpServiceAccountPlan.PrivateKeyId.Value, gcpServiceAccountPlan.ClientEmail.Value, gcpServiceAccountPlan.ClientId.Value, gcpServiceAccountPlan.AuthUri.Value, gcpServiceAccountPlan.TokenUri.Value, gcpServiceAccountPlan.AuthProviderX509CertUrl.Value, gcpServiceAccountPlan.ClientX509CertUrl.Value)
		if !gcpServiceAccountPlan.UniverseDomain.Null && !gcpServiceAccountPlan.UniverseDomain.Unknown && gcpServiceAccountPlan.UniverseDomain.Value != "" {
			googleCloudSpec.SetUniverseDomain(gcpServiceAccountPlan.UniverseDomain.Value)
		}
		telemetryProviderSpec.SetGooglecloudSpec(googleCloudSpec)
	default:
		//We should never go there normally
		resp.Diagnostics.AddError(
			"Only DATADOG, GRAFANA, SUMOLOGIC and GOOGLECLOUD are currently supported as an integrations",
			"",
		)
		return
	}

	CreateResp, _, err := apiClient.TelemetryProviderApi.CreateTelemetryProvider(ctx, accountId, projectId).TelemetryProviderSpec(*telemetryProviderSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to create integration", GetApiErrorDetails(err))
		return
	}

	telemetryProvider, readOK, message := resourceTelemetryProviderRead(accountId, projectId, CreateResp.GetData().Info.Id, apiClient, plan)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the integration", message)
		return
	}

	diags := resp.State.Set(ctx, &telemetryProvider)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceIntegration) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state TelemetryProvider

	getIDsFromIntegrationState(ctx, req.State, &state)
	configID := state.ConfigID.Value

	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	config, readOK, message := resourceTelemetryProviderRead(accountId, projectId, configID, apiClient, state)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the integration", message)
		return
	}

	diags := resp.State.Set(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
func (r resourceIntegration) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	resp.Diagnostics.AddError(
		"Unsupported Operation",
		"This resource does not support updates. Please destroy and recreate the resource if changes are needed.",
	)
}

func resourceTelemetryProviderRead(accountId string, projectId string, configID string, apiClient *openapiclient.APIClient, userProvidedTpDetails TelemetryProvider) (tp TelemetryProvider, readOK bool, errorMessage string) {
	// userProvidedTpDetails: Telemetry provider details from the state or plan used to set the credentials which are masked when obtained from the API

	configData, err := GetTelemetryProviderById(accountId, projectId, configID, apiClient)
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

	// API returns masked credentials. We use the credential details provided by the user in the plan or the existing state
	switch configData.GetSpec().Type {
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG:
		tp.DataDogSpec = &DataDogSpec{
			ApiKey: userProvidedTpDetails.DataDogSpec.ApiKey,
			Site:   types.String{Value: configSpec.DatadogSpec.Get().Site},
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_PROMETHEUS:
		tp.PrometheusSpec = &PrometheusSpec{
			Endpoint: types.String{Value: userProvidedTpDetails.PrometheusSpec.Endpoint.Value},
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		grafanaSpec := configSpec.GetGrafanaSpec()
		tp.GrafanaSpec = &GrafanaSpec{
			AccessTokenPolicy: userProvidedTpDetails.GrafanaSpec.AccessTokenPolicy,
			Zone:              types.String{Value: grafanaSpec.Zone},
			InstanceId:        types.String{Value: grafanaSpec.InstanceId},
			OrgSlug:           types.String{Value: grafanaSpec.OrgSlug},
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		tp.SumoLogicSpec = &SumoLogicSpec{
			AccessKey:         userProvidedTpDetails.SumoLogicSpec.AccessKey,
			AccessId:          userProvidedTpDetails.SumoLogicSpec.AccessId,
			InstallationToken: userProvidedTpDetails.SumoLogicSpec.InstallationToken,
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD:
		googlecloudSpec := configSpec.GetGooglecloudSpec()
		tp.GoogleCloudSpec = &GCPServiceAccount{
			Type:                    types.String{Value: googlecloudSpec.Type},
			ProjectId:               types.String{Value: googlecloudSpec.ProjectId},
			PrivateKeyId:            types.String{Value: googlecloudSpec.PrivateKeyId},
			PrivateKey:              userProvidedTpDetails.GoogleCloudSpec.PrivateKey,
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

func (r resourceIntegration) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	apiClient := r.p.client
	var state TelemetryProvider
	getIDsFromIntegrationState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	configId := state.ConfigID.Value

	_, err := apiClient.TelemetryProviderApi.DeleteTelemetryProvider(ctx, accountId, projectId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete the integration", GetApiErrorDetails(err))
		return
	}

	resp.State.RemoveResource(ctx)
}

func GetTelemetryProviderById(accountId string, projectId string, configID string, apiClient *openapiclient.APIClient) (*openapiclient.TelemetryProviderData, error) {
	resp, _, err := apiClient.TelemetryProviderApi.ListTelemetryProviders(context.Background(), accountId, projectId).Execute()

	if err != nil {
		return nil, err
	}

	for _, telemetryProvider := range resp.Data {
		if telemetryProvider.GetInfo().Id == configID {
			return &telemetryProvider, nil
		}
	}

	return nil, fmt.Errorf("could not find integration with id: %s", configID)
}

// Import API Key
func (r resourceIntegration) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
