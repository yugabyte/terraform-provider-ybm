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

type resourceMetricsExporterType struct{}

func (r resourceMetricsExporterType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a metrics exporter config in YugabyteDB Managed.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this metrics exporter config belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this metrics exporter config belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_id": {
				Description: "The ID of the metrics exporter config.",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_name": {
				Description: "The name of the metrics exporter configuration",
				Type:        types.StringType,
				Required:    true,
			},
			"type": {
				Description: "The type of third party metrics sink. ",
				Type:        types.StringType,
				Required:    true,
				Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("DATADOG", "GRAFANA", "SUMOLOGIC")},
			},
			"datadog_spec": {
				Description: "Configuration for Datadog metrics sink.",
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
				Description: "Configuration for Grafana metrics sink.",
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
				Description: "Configuration for Sumologic metrics sink.",
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
						Description: "A SumoLogic installation token to export metrics",
						Type:        types.StringType,
						Required:    true,
						Sensitive:   true,
					},
				}),
			},
		},
	}, nil
}

func (r resourceMetricsExporterType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceMetricsExporter{
		p: *(p.(*provider)),
	}, nil
}

type resourceMetricsExporter struct {
	p provider
}

func getMetricsExporterPlan(ctx context.Context, plan tfsdk.Plan, me *MetricsExporter) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("config_name"), &me.ConfigName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("type"), &me.Type)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("datadog_spec"), &me.DataDogSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("grafana_spec"), &me.GrafanaSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("sumologic_spec"), &me.SumoLogicSpec)...)
	return diags
}

func getIDsFromMetricsExporterState(ctx context.Context, state tfsdk.State, me *MetricsExporter) {
	state.GetAttribute(ctx, path.Root("account_id"), &me.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &me.ProjectID)
	state.GetAttribute(ctx, path.Root("config_id"), &me.ConfigID)
	state.GetAttribute(ctx, path.Root("type"), &me.Type)
	switch me.Type.Value {
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_DATADOG):
		state.GetAttribute(ctx, path.Root("datadog_spec"), &me.DataDogSpec)
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_GRAFANA):
		state.GetAttribute(ctx, path.Root("grafana_spec"), &me.GrafanaSpec)
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_SUMOLOGIC):
		state.GetAttribute(ctx, path.Root("sumologic_spec"), &me.SumoLogicSpec)
	}
}

func (r resourceMetricsExporter) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan MetricsExporter
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getMetricsExporterPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the metrics exporter")
		return
	}

	if plan.ConfigID.Value != "" {
		resp.Diagnostics.AddError(
			"Metrics exporter Config ID provided for new metrics exporter config",
			"The config_id was provided even though a new metrics exporter config is being created. Please include this field in the provider when creating it.",
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

	metricsSinkTypeEnum, err := openapiclient.NewMetricsExporterConfigTypeEnumFromValue(strings.ToUpper(sinkType))
	if err != nil {
		resp.Diagnostics.AddError(GetApiErrorDetails(err), "")
		return
	}

	metricsExporterConfigSpec := openapiclient.NewMetricsExporterConfigurationSpec(configName, *metricsSinkTypeEnum)
	apiKey := ""
	var sumoSpec *SumoLogicSpec
	switch *metricsSinkTypeEnum {
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_DATADOG:
		if plan.DataDogSpec == nil {
			resp.Diagnostics.AddError(
				"datadog_spec is required for type DATADOG",
				"datadog_spec is required when third party sink is DATADOG. Please include this field in the resource",
			)
			return
		}
		datadogSpec := openapiclient.NewDatadogMetricsExporterConfigurationSpec(plan.DataDogSpec.ApiKey.Value, plan.DataDogSpec.Site.Value)
		metricsExporterConfigSpec.SetDatadogSpec(*datadogSpec)
		apiKey = plan.DataDogSpec.ApiKey.Value
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_GRAFANA:
		if plan.GrafanaSpec == nil {
			resp.Diagnostics.AddError(
				"grafana_spec is required for type GRAFANA",
				"grafana_spec is required when third party sink is GRAFANA. Please include this field in the resource",
			)
			return
		}
		grafanaSpec := openapiclient.NewGrafanaMetricsExporterConfigurationSpec(plan.GrafanaSpec.AccessTokenPolicy.Value, plan.GrafanaSpec.Zone.Value, plan.GrafanaSpec.InstanceId.Value, plan.GrafanaSpec.OrgSlug.Value)
		metricsExporterConfigSpec.SetGrafanaSpec(*grafanaSpec)
		apiKey = plan.GrafanaSpec.AccessTokenPolicy.Value
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_SUMOLOGIC:
		if plan.SumoLogicSpec == nil {
			resp.Diagnostics.AddError(
				"sumologic_spec is required for type SUMOLOGIC",
				"sumologic_spec is required when third party sink is SUMOLOGIC. Please include this field in the resource",
			)
			return
		}
		sumoLogicSpec := openapiclient.NewSumologicMetricsExporterConfigurationSpec(plan.SumoLogicSpec.InstallationToken.Value, plan.SumoLogicSpec.AccessId.Value, plan.SumoLogicSpec.AccessKey.Value)
		metricsExporterConfigSpec.SetSumologicSpec(*sumoLogicSpec)
		sumoSpec = plan.SumoLogicSpec
	default:
		//We should never go there normally
		resp.Diagnostics.AddError(
			"Only DATADOG,GRAFANA,SUMOLOGIC are currently supported as a third party sink",
			"",
		)
		return
	}

	CreateResp, _, err := apiClient.MetricsExporterConfigApi.CreateMetricsExporterConfig(ctx, accountId, projectId).MetricsExporterConfigurationSpec(*metricsExporterConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("unable to create metrics exporter config", GetApiErrorDetails(err))
		return
	}

	metricsExporterId := CreateResp.GetData().Info.Id

	config, readOK, message := resourceMetricsExporterRead(accountId, projectId, metricsExporterId, "", apiKey, apiClient, sumoSpec)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the metrics exporter config ", message)
		return
	}

	diags := resp.State.Set(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

func (r resourceMetricsExporter) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state MetricsExporter

	getIDsFromMetricsExporterState(ctx, req.State, &state)
	configID := state.ConfigID.Value
	apiKey := ""
	var sumoSpec *SumoLogicSpec

	// We cannot use the api return as the apikey return by the api is masked.
	// We need to use the one provided by the user which should be in the state
	switch state.Type.Value {
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_DATADOG):
		apiKey = state.DataDogSpec.ApiKey.Value
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_GRAFANA):
		apiKey = state.GrafanaSpec.AccessTokenPolicy.Value
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_SUMOLOGIC):
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

	config, readOK, message := resourceMetricsExporterRead(accountId, projectId, configID, "", apiKey, apiClient, sumoSpec)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the metrics exporter config ", message)
		return
	}
	// If value returned by API is the same as the encrypted version of our KEY
	// then we use the api key in the state
	switch state.Type.Value {
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_DATADOG):
		if config.DataDogSpec.ApiKey.Value == state.DataDogSpec.EncryptedKey() {
			config.DataDogSpec.ApiKey.Value = apiKey

		}
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_GRAFANA):
		if config.GrafanaSpec.AccessTokenPolicy.Value == state.GrafanaSpec.EncryptedKey() {
			config.GrafanaSpec.AccessTokenPolicy.Value = apiKey
		}
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_SUMOLOGIC):
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
func (r resourceMetricsExporter) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan MetricsExporter
	resp.Diagnostics.Append(getMetricsExporterPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the metrics exporter config")
		return
	}

	apiClient := r.p.client
	var state MetricsExporter
	getIDsFromMetricsExporterState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	configId := state.ConfigID.Value
	sinkType := plan.Type.Value
	configName := plan.ConfigName.Value

	metricsSinkTypeEnum, err := openapiclient.NewMetricsExporterConfigTypeEnumFromValue(strings.ToUpper(sinkType))
	if err != nil {
		resp.Diagnostics.AddError(GetApiErrorDetails(err), "")
		return
	}

	metricsExporterConfigSpec := openapiclient.NewMetricsExporterConfigurationSpec(configName, *metricsSinkTypeEnum)
	apiKey := ""
	var sumoSpec *SumoLogicSpec
	switch *metricsSinkTypeEnum {
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_DATADOG:
		if plan.DataDogSpec == nil {
			resp.Diagnostics.AddError(
				"datadog_spec is required for type DATADOG",
				"datadog_spec is required when third party sink is DATADOG. Please include this field in the resource",
			)
			return
		}
		datadogSpec := openapiclient.NewDatadogMetricsExporterConfigurationSpec(plan.DataDogSpec.ApiKey.Value, plan.DataDogSpec.Site.Value)
		metricsExporterConfigSpec.SetDatadogSpec(*datadogSpec)
		apiKey = plan.DataDogSpec.ApiKey.Value
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_GRAFANA:
		if plan.GrafanaSpec == nil {
			resp.Diagnostics.AddError(
				"grafana_spec is required for type GRAFANA",
				"grafana_spec is required when third party sink is GRAFANA. Please include this field in the resource",
			)
			return
		}
		grafanaSpec := openapiclient.NewGrafanaMetricsExporterConfigurationSpec(plan.GrafanaSpec.AccessTokenPolicy.Value, plan.GrafanaSpec.Zone.Value, plan.GrafanaSpec.InstanceId.Value, plan.GrafanaSpec.OrgSlug.Value)
		metricsExporterConfigSpec.SetGrafanaSpec(*grafanaSpec)
		apiKey = plan.GrafanaSpec.AccessTokenPolicy.Value
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_SUMOLOGIC:
		if plan.SumoLogicSpec == nil {
			resp.Diagnostics.AddError(
				"sumologic_spec is required for type SUMOLOGIC",
				"sumologic_spec is required when third party sink is SUMOLOGIC. Please include this field in the resource",
			)
			return
		}
		sumoLogicSpec := openapiclient.NewSumologicMetricsExporterConfigurationSpec(plan.SumoLogicSpec.InstallationToken.Value, plan.SumoLogicSpec.AccessId.Value, plan.SumoLogicSpec.AccessKey.Value)
		metricsExporterConfigSpec.SetSumologicSpec(*sumoLogicSpec)
		sumoSpec = plan.SumoLogicSpec
	default:
		//We should never go there normally
		resp.Diagnostics.AddError(
			"Only DATADOG, GRAFANA and SUMOLOGIC are currently supported as a third party sink",
			"",
		)
		return
	}

	updateResp, _, err := apiClient.MetricsExporterConfigApi.UpdateMetricsExporterConfig(ctx, accountId, projectId, configId).MetricsExporterConfigurationSpec(*metricsExporterConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("unable to update metrics exporter config", GetApiErrorDetails(err))
		return
	}

	metricsExporterId := updateResp.GetData().Info.Id

	config, readOK, message := resourceMetricsExporterRead(accountId, projectId, metricsExporterId, "", apiKey, apiClient, sumoSpec)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the metrics exporter config ", message)
		return
	}

	// If value returned by API is the same as the encrypted version of our KEY
	// then we use the api key in the plan
	switch plan.Type.Value {
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_DATADOG):
		if config.DataDogSpec.ApiKey.Value == plan.DataDogSpec.EncryptedKey() {
			config.DataDogSpec.ApiKey.Value = apiKey
		}
	case string(openapiclient.METRICSEXPORTERCONFIGTYPEENUM_GRAFANA):
		if config.GrafanaSpec.AccessTokenPolicy.Value == plan.GrafanaSpec.EncryptedKey() {
			config.GrafanaSpec.AccessTokenPolicy.Value = apiKey
		}
	}
	diags := resp.State.Set(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

func resourceMetricsExporterRead(accountId string, projectId string, configID string, configName string, apiKey string, apiClient *openapiclient.APIClient, sumoSpec *SumoLogicSpec) (me MetricsExporter, readOK bool, errorMessage string) {

	config, err := GetConfigByNameorID(accountId, projectId, configID, configName, apiClient)
	if err != nil {
		return me, false, GetApiErrorDetails(err)
	}

	me.AccountID.Value = accountId
	me.ProjectID.Value = projectId
	me.ConfigName.Value = config.GetSpec().Name
	me.ConfigID.Value = config.GetInfo().Id
	me.Type.Value = string(config.GetSpec().Type)
	switch config.GetSpec().Type {
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_DATADOG:
		// We cannot use the api return as the apikey return by the api is masked.
		// We need to use the one provided by the user
		me.DataDogSpec = &DataDogSpec{
			ApiKey: types.String{Value: apiKey},
			Site:   types.String{Value: config.GetSpec().DatadogSpec.Get().Site},
		}
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_GRAFANA:
		// We cannot use the api return as the apikey return by the api is masked.
		// We need to use the one provided by the user
		me.GrafanaSpec = &GrafanaSpec{
			AccessTokenPolicy: types.String{Value: string(apiKey)},
			Zone:              types.String{Value: string(config.GetSpec().GrafanaSpec.Get().Zone)},
			InstanceId:        types.String{Value: string(config.GetSpec().GrafanaSpec.Get().InstanceId)},
			OrgSlug:           types.String{Value: string(config.GetSpec().GrafanaSpec.Get().OrgSlug)},
		}
	case openapiclient.METRICSEXPORTERCONFIGTYPEENUM_SUMOLOGIC:
		me.SumoLogicSpec = sumoSpec
	}

	return me, true, ""

}

func (r resourceMetricsExporter) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	apiClient := r.p.client
	var state MetricsExporter
	getIDsFromMetricsExporterState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	configId := state.ConfigID.Value

	_, err := apiClient.MetricsExporterConfigApi.DeleteMetricsExporterConfig(ctx, accountId, projectId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("unable to delete metrics exporter config", GetApiErrorDetails(err))
		return
	}

	resp.State.RemoveResource(ctx)

}

func GetConfigByNameorID(accountId string, projectId string, configID string, configName string, apiClient *openapiclient.APIClient) (*openapiclient.MetricsExporterConfigurationData, error) {
	resp, _, err := apiClient.MetricsExporterConfigApi.ListMetricsExporterConfigs(context.Background(), accountId, projectId).Execute()

	if err != nil {
		return nil, err
	}

	if len(configName) > 1 {
		for _, metricsExporter := range resp.Data {
			if metricsExporter.GetSpec().Name == configName {
				return &metricsExporter, nil
			}
		}
	}

	if len(configID) > 1 {
		for _, metricsExporter := range resp.Data {
			if metricsExporter.GetInfo().Id == configID {
				return &metricsExporter, nil
			}
		}
	}

	return nil, fmt.Errorf("could not find metrics export: %s", strings.TrimSpace(fmt.Sprintf("%s %s", configID, configName)))
}

func GetClusterByNameorID(accountId string, projectId string, clusterID string, clusterName string, apiClient *openapiclient.APIClient) (*openapiclient.ClusterData, error) {
	if len(clusterName) > 1 {
		res, _, err := apiClient.ClusterApi.ListClusters(context.Background(), accountId, projectId).Name(clusterName).Execute()

		if err != nil {
			return nil, err
		}
		if len(res.Data) > 0 {
			return &res.GetData()[0], nil
		}
	}

	if len(clusterID) > 1 {
		clusterResp, _, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterID).Execute()
		if err != nil {
			return nil, err
		}
		return clusterResp.Data, nil

	}

	return nil, fmt.Errorf("could not find cluster: %s", strings.TrimSpace(fmt.Sprintf("%s %s", clusterID, clusterName)))
}

// Import API Key
func (r resourceMetricsExporter) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
