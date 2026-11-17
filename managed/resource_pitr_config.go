/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourcePitrConfig) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The resource to create a PITR Config for a namespace in YugabyteDB Aeon.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this PITR Config belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this PITR Config belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"cluster_id": schema.StringAttribute{
				Description: "The ID of the cluster this PITR Config belongs to.",
				Required:    true,
			},
			"pitr_config_id": schema.StringAttribute{
				Description: "The ID of the PITR Config.",
				Computed:    true,
				Optional:    true,
			},
			"namespace_id": schema.StringAttribute{
				Description: "The ID of the namespace that this PITR Config is associated to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"namespace_name": schema.StringAttribute{
				Description: "The namespace name for the PITR Config.",
				Required:    true,
			},
			"namespace_type": schema.StringAttribute{
				Description: "The namespace type.",
				Required:    true,
			},
			"retention_period_in_days": schema.Int64Attribute{
				Description: "The retention period of the PITR Config.",
				Required:    true,
			},
			"state": schema.StringAttribute{
				Description: "The status of the PITR config.",
				Computed:    true,
			},
			"earliest_recovery_time_millis": schema.Int64Attribute{
				Description: "The earliest recovery time in milliseconds to which the namespace can be restored.",
				Computed:    true,
			},
			"latest_recovery_time_millis": schema.Int64Attribute{
				Description: "The latest recovery time in milliseconds to which the namespace can be restored.",
				Computed:    true,
			},
		},
	}
}

func newResourcePitrConfig() resource.Resource {
	return &resourcePitrConfig{}
}

func (r *resourcePitrConfig) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pitr_config"
}

func (r *resourcePitrConfig) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourcePitrConfig struct {
	p provider
}

func GetNamespaceTypeMap() map[string]string {
	return map[string]string{
		"YSQL": "PGSQL_TABLE_TYPE",
		"YCQL": "YQL_TABLE_TYPE",
	}
}

func createBulkPitrConfigRequest(apiClient *openapiclient.APIClient, namespaceId string, retentionPeriod int32) (pitrConfigsRequest *openapiclient.BulkCreateDatabasePitrConfigSpec) {

	pitrConfigSpecs := []openapiclient.DatabasePitrConfigSpec{}
	pitrConfigSpecs = append(pitrConfigSpecs, *openapiclient.NewDatabasePitrConfigSpec(namespaceId, retentionPeriod))

	pitrConfigsRequest = openapiclient.NewBulkCreateDatabasePitrConfigSpec()
	pitrConfigsRequest.SetPitrConfigSpecs(pitrConfigSpecs)

	return pitrConfigsRequest
}

func getPitrConfigPlan(ctx context.Context, plan tfsdk.Plan, pitrConfig *PitrConfig) diag.Diagnostics {
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &pitrConfig.ClusterId)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("namespace_name"), &pitrConfig.NamespaceName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("namespace_type"), &pitrConfig.NamespaceType)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("retention_period_in_days"), &pitrConfig.RetentionPeriodInDays)...)

	return diags
}

func getPitrConfigState(ctx context.Context, state tfsdk.State, pitrConfig *PitrConfig) {
	state.GetAttribute(ctx, path.Root("account_id"), &pitrConfig.AccountId)
	state.GetAttribute(ctx, path.Root("project_id"), &pitrConfig.ProjectId)
	state.GetAttribute(ctx, path.Root("cluster_id"), &pitrConfig.ClusterId)
	state.GetAttribute(ctx, path.Root("pitr_config_id"), &pitrConfig.PitrConfigId)
	state.GetAttribute(ctx, path.Root("namespace_id"), &pitrConfig.NamespaceId)
	state.GetAttribute(ctx, path.Root("namespace_name"), &pitrConfig.NamespaceName)
	state.GetAttribute(ctx, path.Root("namespace_type"), &pitrConfig.NamespaceType)
	state.GetAttribute(ctx, path.Root("retention_period_in_days"), &pitrConfig.RetentionPeriodInDays)
	state.GetAttribute(ctx, path.Root("state"), &pitrConfig.State)
	state.GetAttribute(ctx, path.Root("earliest_recovery_time_millis"), &pitrConfig.EarliestRecoveryTimeMillis)
	state.GetAttribute(ctx, path.Root("latest_recovery_time_millis"), &pitrConfig.LatestRecoveryTimeMillis)
}

func resourcePitrConfigRead(accountId string, projectId string, clusterId string, pitrConfigId string, apiClient *openapiclient.APIClient) (pitrConfig PitrConfig, readOK bool, errorMessage string) {
	pitrConfigResp, response, err := apiClient.ClusterApi.GetDatabasePitrConfig(context.Background(), accountId, projectId, clusterId, pitrConfigId).Execute()
	if err != nil {
		if response != nil && response.StatusCode == 404 {
			return pitrConfig, false, "Delete resource"
		}
		errMsg := getErrorMessage(response, err)
		return pitrConfig, false, errMsg
	}

	pitrConfig.AccountId = types.StringValue(accountId)
	pitrConfig.ProjectId = types.StringValue(projectId)
	pitrConfig.ClusterId = types.StringValue(clusterId)
	pitrConfig.PitrConfigId = types.StringValue(pitrConfigId)
	pitrConfig.NamespaceId = types.StringValue(pitrConfigResp.Data.Spec.DatabaseId)
	pitrConfig.NamespaceName = types.StringValue(pitrConfigResp.Data.Info.GetDatabaseName())
	pitrConfig.NamespaceType = types.StringValue(string(pitrConfigResp.Data.Info.GetDatabaseType()))
	pitrConfig.RetentionPeriodInDays = types.Int64Value(int64(pitrConfigResp.Data.Spec.RetentionPeriod))
	pitrConfig.State = types.StringValue(pitrConfigResp.Data.Info.GetState())
	pitrConfig.EarliestRecoveryTimeMillis = types.Int64Value(pitrConfigResp.Data.Info.GetEarliestRecoveryTimeMillis())
	pitrConfig.LatestRecoveryTimeMillis = types.Int64Value(pitrConfigResp.Data.Info.GetLatestRecoveryTimeMillis())

	return pitrConfig, true, ""
}

// Create PITR Config
func (r resourcePitrConfig) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan PitrConfig
	var accountId, message string
	var getAccountOK bool
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(getPitrConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the PITR Configs")
		return
	}

	if (!plan.PitrConfigId.IsUnknown() && !plan.PitrConfigId.IsNull()) || plan.PitrConfigId.ValueString() != "" {
		resp.Diagnostics.AddError(
			"PITR Config ID provided for new PITR Config",
			"The pitr_config_id was provided even though a new PITR Config is being created. Do not include this field in the provider when creating a PITR Config.",
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
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	clusterId := plan.ClusterId.ValueString()
	namespaceName := plan.NamespaceName.ValueString()
	namespaceType := plan.NamespaceType.ValueString()

	namespacesResp, response, err := apiClient.ClusterApi.GetClusterNamespaces(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create PITR configuration", errMsg)
		return
	}

	var namespaceId string

	for _, namespace := range namespacesResp.Data {
		if namespace.GetName() == namespaceName && namespace.GetTableType() == GetNamespaceTypeMap()[namespaceType] {
			namespaceId = namespace.GetId()
		}
	}
	if len(namespaceId) == 0 {
		msg := "No " + namespaceType + " namespace found with name " + namespaceName
		resp.Diagnostics.AddError("Unable to create PITR config:", msg)
		return
	}

	retentionPeriod := int32(plan.RetentionPeriodInDays.ValueInt64())
	if retentionPeriod < 2 || retentionPeriod > 14 {
		resp.Diagnostics.AddError("Unable to create PITR config:", "Retention period must be between 2 and 14 days")
		return
	}

	createPitrConfigsRequest := createBulkPitrConfigRequest(apiClient, namespaceId, retentionPeriod)

	pitrConfigsResp, response, err := apiClient.ClusterApi.CreateDatabasePitrConfig(context.Background(), accountId, projectId, clusterId).BulkCreateDatabasePitrConfigSpec(*createPitrConfigsRequest).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create PITR Config ", errMsg)
		return
	}

	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_BULK_ENABLE_DB_PITR, apiClient, ctx)

		tflog.Info(ctx, "PITR config creation operation in progress, state: "+asState)

		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("PITR config creation operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for PITR config creation to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "PITR config creation operation failed"
		}
		resp.Diagnostics.AddError("Unable to create PITR config:", msg)
		return
	}

	pitrconfig, readOK, message := resourcePitrConfigRead(accountId, projectId, clusterId, pitrConfigsResp.GetData()[0].Info.GetId(), apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the PITR config after creation", message)
		if message == "Delete Resource" {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	tflog.Debug(ctx, "PITR Config Create: PITR Config on read from API server", map[string]interface{}{
		"PITR Config": pitrconfig})

	diags = resp.State.Set(ctx, &pitrconfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read PITR configuration
func (r resourcePitrConfig) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PitrConfig
	getPitrConfigState(ctx, req.State, &state)

	apiClient := r.p.client
	accountId := state.AccountId.ValueString()
	projectId := state.ProjectId.ValueString()
	clusterId := state.ClusterId.ValueString()
	pitrConfigId := state.PitrConfigId.ValueString()

	pitrconfig, readOK, message := resourcePitrConfigRead(accountId, projectId, clusterId, pitrConfigId, apiClient)
	if !readOK {
		if message == "Delete Resource" {
			resp.State.RemoveResource(ctx)
		}
		resp.Diagnostics.AddError("Unable to read the state of the PITR config ", message)
		return
	}
	tflog.Debug(ctx, "PITR Config on read from API server", map[string]interface{}{
		"PITR Config": pitrconfig})

	diags := resp.State.Set(ctx, &pitrconfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update PITR Config
func (r resourcePitrConfig) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PitrConfig
	var state PitrConfig

	// Get current state
	getPitrConfigState(ctx, req.State, &state)

	// Get planned changes
	resp.Diagnostics.Append(getPitrConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the PITR config edit")
		return
	}

	// Verify that only retention period field is being changed
	if plan.ClusterId.ValueString() != state.ClusterId.ValueString() || plan.NamespaceName.ValueString() != state.NamespaceName.ValueString() || plan.NamespaceType.ValueString() != state.NamespaceType.ValueString() {
		resp.Diagnostics.AddError(
			"Invalid edit to PITR configuration",
			"Only the retention period field can be modified in PITR configurations. Other fields cannot be changed.",
		)
		return
	}

	apiClient := r.p.client
	accountId := state.AccountId.ValueString()
	projectId := state.ProjectId.ValueString()
	clusterId := state.ClusterId.ValueString()
	pitrConfigId := state.PitrConfigId.ValueString()

	retentionPeriod := int32(plan.RetentionPeriodInDays.ValueInt64())
	if retentionPeriod < 2 || retentionPeriod > 14 {
		resp.Diagnostics.AddError("Unable to update PITR config:", "Retention period must be between 2 and 14 days")
		return
	}

	// Create edit request with new retention period
	_, response, err := apiClient.ClusterApi.UpdateDatabasePitrConfig(ctx, accountId, projectId, clusterId, pitrConfigId).UpdateDatabasePitrConfigSpec(*openapiclient.NewUpdateDatabasePitrConfigSpec(retentionPeriod)).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to edit PITR configuration", errMsg)
		return
	}

	// Wait for  to complete
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_UPDATE_DB_PITR, apiClient, ctx)

		tflog.Info(ctx, "PITR config edit operation in progress, state: "+asState)

		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("PITR config edit operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for PITR config edit to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "PITR config edit operation failed"
		}
		resp.Diagnostics.AddError("Unable to edit PITR config:", msg)
		return
	}

	pitrconfig, readOK, message := resourcePitrConfigRead(accountId, projectId, clusterId, pitrConfigId, apiClient)
	if !readOK {
		if message == "Delete Resource" {
			resp.State.RemoveResource(ctx)
		}
		resp.Diagnostics.AddError("Unable to read the state of the PITR config after update", message)
		return
	}
	tflog.Debug(ctx, "PITR Config Update: PITR Config on read from API server", map[string]interface{}{
		"PITR Config": pitrconfig})

	diags := resp.State.Set(ctx, &pitrconfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete PITR configuration
func (r resourcePitrConfig) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PitrConfig
	getPitrConfigState(ctx, req.State, &state)
	accountId := state.AccountId.ValueString()
	projectId := state.ProjectId.ValueString()
	clusterId := state.ClusterId.ValueString()
	pitrConfigId := state.PitrConfigId.ValueString()

	apiClient := r.p.client

	_, err := apiClient.ClusterApi.RemoveDatabasePitrConfig(ctx, accountId, projectId, clusterId, pitrConfigId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete PITR configuration", GetApiErrorDetails(err))
		return
	}

	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_DISABLE_DB_PITR, apiClient, ctx)

		tflog.Info(ctx, "PITR config delete operation in progress, state: "+asState)

		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("PITR config deletion operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for PITR config deletion to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "PITR config deletion operation failed"
		}
		resp.Diagnostics.AddError("Unable to delete PITR config:", msg)
		return
	}

	resp.State.RemoveResource(ctx)
}

// Import PITR Config
func (r resourcePitrConfig) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
