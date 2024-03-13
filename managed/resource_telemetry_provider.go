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
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceTelemetryProviderType struct{}

func (r resourceTelemetryProviderType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a telemetry provider config in YugabyteDB Managed.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this telemetry provider config belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this telemetry provider config belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_id": {
				Description: "The ID of the telemetry provider config.",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_name": {
				Description: "The name of the telemetry provider configuration",
				Type:        types.StringType,
				Required:    true,
			},
			"type": {
				Description: "Defines different exporter destination types. ",
				Type:        types.StringType,
				Required:    true,
				Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("DATADOG", "GRAFANA", "SUMOLOGIC")},
			},
			"is_valid": {
				Description: "Signifies whether the configuration is valid or not ",
				Type:        types.BoolType,
				Computed:    true,
			},
			"datadog_spec": {
				Description: "The specifications of a Datadog telemetry provider.",
				Optional:    true,
				Validators: []tfsdk.AttributeValidator{
					schemavalidator.ConflictsWith(path.MatchRoot("grafana_spec")),
					schemavalidator.ConflictsWith(path.MatchRoot("sumologic_spec")),
				},
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
			"grafana_spec": {
				Description: "The specifications of a Grafana telemetry provider.",
				Optional:    true,
				Validators: []tfsdk.AttributeValidator{
					schemavalidator.ConflictsWith(path.MatchRoot("datadog_spec")),
					schemavalidator.ConflictsWith(path.MatchRoot("sumologic_spec")),
				},
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
				Description: "The specifications of a Sumo Logic telemetry provider.",
				Optional:    true,
				Validators: []tfsdk.AttributeValidator{
					schemavalidator.ConflictsWith(path.MatchRoot("datadog_spec")),
					schemavalidator.ConflictsWith(path.MatchRoot("grafana_spec")),
				},
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
		},
	}, nil
}

func (r resourceTelemetryProviderType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceTelemetryProvider{
		p: *(p.(*provider)),
	}, nil
}

type resourceTelemetryProvider struct {
	p provider
}

func getTelemetryProviderPlan(ctx context.Context, plan tfsdk.Plan, tp *TelemetryProvider) diag.Diagnostics { // TODO Sid - Replace me
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("config_name"), &tp.ConfigName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("type"), &tp.Type)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("datadog_spec"), &tp.DataDogSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("grafana_spec"), &tp.GrafanaSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("sumologic_spec"), &tp.SumoLogicSpec)...)
	return diags
}

func getIDsFromTelemetryProviderState(ctx context.Context, state tfsdk.State, tp *TelemetryProvider) {
	state.GetAttribute(ctx, path.Root("account_id"), &tp.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &tp.ProjectID)
	state.GetAttribute(ctx, path.Root("config_id"), &tp.ConfigID)
	state.GetAttribute(ctx, path.Root("type"), &tp.Type)
	switch tp.Type.Value {
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG):
		state.GetAttribute(ctx, path.Root("datadog_spec"), &tp.DataDogSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA):
		state.GetAttribute(ctx, path.Root("grafana_spec"), &tp.GrafanaSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC):
		state.GetAttribute(ctx, path.Root("sumologic_spec"), &tp.SumoLogicSpec)
	}
}

func (r resourceTelemetryProvider) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan TelemetryProvider
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getTelemetryProviderPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the telemetry provider")
		return
	}

	if plan.ConfigID.Value != "" {
		resp.Diagnostics.AddError(
			"Telemetry provider Config ID provided for new telemetry provider config",
			"The config_id was provided even though a new telemetry provider config is being created. Please include this field in the provider when creating it.",
		)
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get the project ID", message)
		return
	}

	sinkType := plan.Type.Value
	configName := plan.ConfigName.Value

	telemetrySinkTypeEnum, err := openapiclient.NewTelemetryProviderTypeEnumFromValue(strings.ToUpper(sinkType))
	if err != nil {
		resp.Diagnostics.AddError(GetApiErrorDetails(err), "")
		return
	}

	telemetryProviderSpec := openapiclient.NewTelemetryProviderSpec(configName, *telemetrySinkTypeEnum)
	var apiKey string
	var sumoSpec *SumoLogicSpec
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
		apiKey = plan.DataDogSpec.ApiKey.Value
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		if plan.GrafanaSpec == nil {
			resp.Diagnostics.AddError(
				"grafana_spec is required for type GRAFANA",
				"grafana_spec is required when telemetry sink is GRAFANA. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetGrafanaSpec(*openapiclient.NewGrafanaTelemetryProviderSpec(plan.GrafanaSpec.AccessTokenPolicy.Value, plan.GrafanaSpec.Zone.Value, plan.GrafanaSpec.InstanceId.Value, plan.GrafanaSpec.OrgSlug.Value))
		apiKey = plan.GrafanaSpec.AccessTokenPolicy.Value
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		if plan.SumoLogicSpec == nil {
			resp.Diagnostics.AddError(
				"sumologic_spec is required for type SUMOLOGIC",
				"sumologic_spec is required when telemetry sink is SUMOLOGIC. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetSumologicSpec(*openapiclient.NewSumologicTelemetryProviderSpec(plan.SumoLogicSpec.InstallationToken.Value, plan.SumoLogicSpec.AccessId.Value, plan.SumoLogicSpec.AccessKey.Value))
		sumoSpec = plan.SumoLogicSpec
	default:
		//We should never go there normally
		resp.Diagnostics.AddError(
			"Only DATADOG,GRAFANA,SUMOLOGIC are currently supported as a third party sink",
			"",
		)
		return
	}

	CreateResp, _, err := apiClient.TelemetryProviderApi.CreateTelemetryProvider(ctx, accountId, projectId).TelemetryProviderSpec(*telemetryProviderSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to create telemetry provider", GetApiErrorDetails(err))
		return
	}

	telemetryProviderId := CreateResp.GetData().Info.Id

	telemetryProvider, readOK, message := resourceTelemetryProviderRead(accountId, projectId, telemetryProviderId, "", apiKey, apiClient, sumoSpec)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the telemetry provider config ", message)
		return
	}

	diags := resp.State.Set(ctx, &telemetryProvider)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceTelemetryProvider) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state TelemetryProvider

	getIDsFromTelemetryProviderState(ctx, req.State, &state)
	configID := state.ConfigID.Value
	var apiKey string
	var sumoSpec *SumoLogicSpec

	// We cannot use the api return as the apikey returned by the api is masked.
	// We need to use the one provided by the user which should be in the state
	switch state.Type.Value {
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG):
		apiKey = state.DataDogSpec.ApiKey.Value
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA):
		apiKey = state.GrafanaSpec.AccessTokenPolicy.Value
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC):
		sumoSpec = state.SumoLogicSpec
	}

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

	config, readOK, message := resourceTelemetryProviderRead(accountId, projectId, configID, "", apiKey, apiClient, sumoSpec)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the telemetry provider config ", message)
		return
	}
	// If value returned by API is the same as the encrypted version of our KEY
	// then we use the api key in the state
	switch state.Type.Value {
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG):
		if config.DataDogSpec.ApiKey.Value == state.DataDogSpec.EncryptedKey() {
			config.DataDogSpec.ApiKey.Value = apiKey

		}
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA):
		if config.GrafanaSpec.AccessTokenPolicy.Value == state.GrafanaSpec.EncryptedKey() {
			config.GrafanaSpec.AccessTokenPolicy.Value = apiKey
		}
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC):
		if config.SumoLogicSpec.AccessKey.Value == state.SumoLogicSpec.EncryptedKey("access_key") {
			config.SumoLogicSpec.AccessKey.Value = sumoSpec.AccessKey.Value
		}
		if config.SumoLogicSpec.AccessId.Value == state.SumoLogicSpec.EncryptedKey("access_id") {
			config.SumoLogicSpec.AccessId.Value = sumoSpec.AccessId.Value
		}
		if config.SumoLogicSpec.InstallationToken.Value == state.SumoLogicSpec.EncryptedKey("installation_token") {
			config.SumoLogicSpec.InstallationToken.Value = sumoSpec.InstallationToken.Value
		}
	}

	diags := resp.State.Set(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
func (r resourceTelemetryProvider) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan TelemetryProvider
	resp.Diagnostics.Append(getTelemetryProviderPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the telemetry provider config")
		return
	}

	apiClient := r.p.client
	var state TelemetryProvider
	getIDsFromTelemetryProviderState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	configId := state.ConfigID.Value
	providerType := plan.Type.Value
	configName := plan.ConfigName.Value

	telemetryProviderTypeEnum, err := openapiclient.NewTelemetryProviderTypeEnumFromValue(strings.ToUpper(providerType))
	if err != nil {
		resp.Diagnostics.AddError(GetApiErrorDetails(err), "")
		return
	}

	telemetryProviderSpec := openapiclient.NewTelemetryProviderSpec(configName, *telemetryProviderTypeEnum)
	var apiKey string
	var sumoSpec *SumoLogicSpec
	switch *telemetryProviderTypeEnum {
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG:
		if plan.DataDogSpec == nil {
			resp.Diagnostics.AddError(
				"datadog_spec is required for type DATADOG",
				"datadog_spec is required when third party sink is DATADOG. Please include this field in the resource",
			)
			return
		}
		datadogSpec := openapiclient.NewDatadogTelemetryProviderSpec(plan.DataDogSpec.ApiKey.Value, plan.DataDogSpec.Site.Value)
		telemetryProviderSpec.SetDatadogSpec(*datadogSpec)
		apiKey = plan.DataDogSpec.ApiKey.Value
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		if plan.GrafanaSpec == nil {
			resp.Diagnostics.AddError(
				"grafana_spec is required for type GRAFANA",
				"grafana_spec is required when third party sink is GRAFANA. Please include this field in the resource",
			)
			return
		}
		grafanaSpec := openapiclient.NewGrafanaTelemetryProviderSpec(plan.GrafanaSpec.AccessTokenPolicy.Value, plan.GrafanaSpec.Zone.Value, plan.GrafanaSpec.InstanceId.Value, plan.GrafanaSpec.OrgSlug.Value)
		telemetryProviderSpec.SetGrafanaSpec(*grafanaSpec)
		apiKey = plan.GrafanaSpec.AccessTokenPolicy.Value
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		if plan.SumoLogicSpec == nil {
			resp.Diagnostics.AddError(
				"sumologic_spec is required for type SUMOLOGIC",
				"sumologic_spec is required when third party sink is SUMOLOGIC. Please include this field in the resource",
			)
			return
		}
		sumoLogicSpec := openapiclient.NewSumologicTelemetryProviderSpec(plan.SumoLogicSpec.InstallationToken.Value, plan.SumoLogicSpec.AccessId.Value, plan.SumoLogicSpec.AccessKey.Value)
		telemetryProviderSpec.SetSumologicSpec(*sumoLogicSpec)
		sumoSpec = plan.SumoLogicSpec
	default:
		//We should never go there normally
		resp.Diagnostics.AddError(
			"Only DATADOG, GRAFANA and SUMOLOGIC are currently supported as a third party sink",
			"",
		)
		return
	}

	updateResp, _, err := apiClient.TelemetryProviderApi.UpdateTelemetryProvider(ctx, accountId, projectId, configId).TelemetryProviderSpec(*telemetryProviderSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to update telemetry provider config", GetApiErrorDetails(err))
		return
	}

	telemetryProviderId := updateResp.GetData().Info.Id

	config, readOK, message := resourceTelemetryProviderRead(accountId, projectId, telemetryProviderId, "", apiKey, apiClient, sumoSpec)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the telemetry provider config ", message)
		return
	}

	// If value returned by API is the same as the encrypted version of our KEY
	// then we use the api key in the plan
	switch plan.Type.Value {
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG):
		if config.DataDogSpec.ApiKey.Value == plan.DataDogSpec.EncryptedKey() {
			config.DataDogSpec.ApiKey.Value = apiKey
		}
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA):
		if config.GrafanaSpec.AccessTokenPolicy.Value == plan.GrafanaSpec.EncryptedKey() {
			config.GrafanaSpec.AccessTokenPolicy.Value = apiKey
		}
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC):
		if config.SumoLogicSpec.AccessKey.Value == plan.SumoLogicSpec.EncryptedKey("access_key") {
			config.SumoLogicSpec.AccessKey.Value = sumoSpec.AccessKey.Value
		}
		if config.SumoLogicSpec.AccessId.Value == plan.SumoLogicSpec.EncryptedKey("access_id") {
			config.SumoLogicSpec.AccessId.Value = sumoSpec.AccessId.Value
		}
		if config.SumoLogicSpec.InstallationToken.Value == plan.SumoLogicSpec.EncryptedKey("installation_token") {
			config.SumoLogicSpec.InstallationToken.Value = sumoSpec.InstallationToken.Value
		}
	}
	diags := resp.State.Set(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

func resourceTelemetryProviderRead(accountId string, projectId string, configID string, configName string, apiKey string, apiClient *openapiclient.APIClient, sumoSpec *SumoLogicSpec) (tp TelemetryProvider, readOK bool, errorMessage string) {

	config, err := GetTelemetryProviderById(accountId, projectId, configID, apiClient)
	if err != nil {
		return tp, false, GetApiErrorDetails(err)
	}

	tp.AccountID.Value = accountId
	tp.ProjectID.Value = projectId
	tp.ConfigName.Value = config.GetSpec().Name
	tp.ConfigID.Value = config.GetInfo().Id
	tp.Type.Value = string(config.GetSpec().Type)
	tp.IsValid.Value = *config.GetInfo().IsValid.Get()
	switch config.GetSpec().Type {
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG:
		// We cannot use the api return as the apikey return by the api is masked.
		// We need to use the one provided by the user
		tp.DataDogSpec = &DataDogSpec{
			ApiKey: types.String{Value: apiKey},
			Site:   types.String{Value: config.GetSpec().DatadogSpec.Get().Site},
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		// We cannot use the api return as the apikey return by the api is masked.
		// We need to use the one provided by the user
		tp.GrafanaSpec = &GrafanaSpec{
			AccessTokenPolicy: types.String{Value: string(apiKey)},
			Zone:              types.String{Value: string(config.GetSpec().GrafanaSpec.Get().Zone)},
			InstanceId:        types.String{Value: string(config.GetSpec().GrafanaSpec.Get().InstanceId)},
			OrgSlug:           types.String{Value: string(config.GetSpec().GrafanaSpec.Get().OrgSlug)},
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		tp.SumoLogicSpec = sumoSpec
	}

	return tp, true, ""
}

func (r resourceTelemetryProvider) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	apiClient := r.p.client
	var state TelemetryProvider
	getIDsFromTelemetryProviderState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	configId := state.ConfigID.Value

	_, err := apiClient.TelemetryProviderApi.DeleteTelemetryProvider(ctx, accountId, projectId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete telemetry provider config", GetApiErrorDetails(err))
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

	return nil, fmt.Errorf("could not find telemetry provider with id: %s", configID)
}

// Import API Key
func (r resourceTelemetryProvider) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
