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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourcePitrConfigType struct{}

func (r resourcePitrConfigType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a PITR Config for a namespace in YugabyteDB Aeon.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this PITR Config belongs to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"project_id": {
				Description: "The ID of the project this PITR Config belongs to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"cluster_id": {
				Description: "The ID of the cluster this PITR Config belongs to.",
				Type:        types.StringType,
				Required:    true,
			},
			"pitr_config_id": {
				Description: "The ID of the PITR Config.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"namespace_id": {
				Description: "The ID of the namespace that this PITR Config is associated to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"namespace_name": {
				Description: "The namespace name for the PITR Config.",
				Type:        types.StringType,
				Required:    true,
			},
			"namespace_type": {
				Description: "The namespace type.",
				Type:        types.StringType,
				Required:    true,
			},
			"retention_period_in_days": {
				Description: "The retention period of the PITR Config.",
				Type:        types.Int64Type,
				Required:    true,
			},
			"state": {
				Description: "The status of the PITR config.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"earliest_recovery_time_millis": {
				Description: "The earliest recovery time in milliseconds to which the namespace can be restored.",
				Type:        types.Int64Type,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"latest_recovery_time_millis": {
				Description: "The latest recovery time in milliseconds to which the namespace can be restored.",
				Type:        types.Int64Type,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
		},
	}, nil
}

func (r resourcePitrConfigType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourcePitrConfig{
		p: *(p.(*provider)),
	}, nil
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

// Create PITR Config
func (r resourcePitrConfig) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

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
	resp.Diagnostics.Append(getPitrConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the PITR Configs")
		return
	}

	if (!plan.PitrConfigId.Unknown && !plan.PitrConfigId.Null) || plan.PitrConfigId.Value != "" {
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

	clusterId := plan.ClusterId.Value
	namespaceName := plan.NamespaceName.Value
	namespaceType := plan.NamespaceType.Value

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
		msg := "No" + namespaceType + "namespace found with name" + namespaceName
		resp.Diagnostics.AddError("Unable to create PITR config:", msg)
	}

	createPitrConfigsRequest := createBulkPitrConfigRequest(apiClient, namespaceId, int32(plan.RetentionPeriodInDays.Value))

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

	// Set the computed fields
	plan.AccountId = types.String{Value: accountId}
	plan.ProjectId = types.String{Value: projectId}
	plan.NamespaceId = types.String{Value: namespaceId}
	plan.PitrConfigId = types.String{Value: pitrConfigsResp.GetData()[0].Info.GetId()}
	plan.State = types.String{Value: pitrConfigsResp.GetData()[0].Info.GetState()}
	plan.State = types.String{Value: pitrConfigsResp.GetData()[0].Info.GetState()}
	plan.EarliestRecoveryTimeMillis = types.Int64{Value: pitrConfigsResp.GetData()[0].Info.GetEarliestRecoveryTimeMillis()}
	plan.LatestRecoveryTimeMillis = types.Int64{Value: pitrConfigsResp.GetData()[0].Info.GetLatestRecoveryTimeMillis()}

	diags := resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read PITR configuration
func (r resourcePitrConfig) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state PitrConfig
	getPitrConfigState(ctx, req.State, &state)

	apiClient := r.p.client
	accountId := state.AccountId.Value
	projectId := state.ProjectId.Value
	clusterId := state.ClusterId.Value
	pitrConfigId := state.PitrConfigId.Value

	pitrConfigResp, response, err := apiClient.ClusterApi.GetDatabasePitrConfig(ctx, accountId, projectId, clusterId, pitrConfigId).Execute()
	if err != nil {
		if response != nil && response.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to read PITR configuration", errMsg)
		return
	}

	// Update state with the current values
	state.NamespaceId = types.String{Value: pitrConfigResp.Data.Spec.DatabaseId}
	state.NamespaceName = types.String{Value: pitrConfigResp.Data.Info.GetDatabaseName()}
	state.NamespaceType = types.String{Value: string(pitrConfigResp.Data.Info.GetDatabaseType())}
	state.RetentionPeriodInDays = types.Int64{Value: int64(pitrConfigResp.Data.Spec.GetRetentionPeriod())}
	state.State = types.String{Value: pitrConfigResp.Data.Info.GetState()}
	state.EarliestRecoveryTimeMillis = types.Int64{Value: pitrConfigResp.Data.Info.GetEarliestRecoveryTimeMillis()}
	state.LatestRecoveryTimeMillis = types.Int64{Value: pitrConfigResp.Data.Info.GetLatestRecoveryTimeMillis()}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update PITR Config
func (r resourcePitrConfig) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
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
	if plan.ClusterId.Value != state.ClusterId.Value || plan.NamespaceName.Value != state.NamespaceName.Value || plan.NamespaceType.Value != state.NamespaceType.Value {
		resp.Diagnostics.AddError(
			"Invalid edit to PITR configuration",
			"Only the retention period field can be modified in PITR configurations. Other fields cannot be changed.",
		)
		return
	}

	apiClient := r.p.client
	accountId := state.AccountId.Value
	projectId := state.ProjectId.Value
	clusterId := state.ClusterId.Value
	pitrConfigId := state.PitrConfigId.Value

	// Create edit request with new retention period
	_, response, err := apiClient.ClusterApi.UpdateDatabasePitrConfig(ctx, accountId, projectId, clusterId, pitrConfigId).UpdateDatabasePitrConfigSpec(*openapiclient.NewUpdateDatabasePitrConfigSpec(int32(plan.RetentionPeriodInDays.Value))).Execute()
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

	// Set state to planned new state
	plan.AccountId = state.AccountId
	plan.ProjectId = state.ProjectId
	plan.PitrConfigId = state.PitrConfigId
	plan.NamespaceId = state.NamespaceId

	diags := resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete PITR configuration
func (r resourcePitrConfig) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state PitrConfig
	getPitrConfigState(ctx, req.State, &state)
	accountId := state.AccountId.Value
	projectId := state.ProjectId.Value
	clusterId := state.ClusterId.Value
	pitrConfigId := state.PitrConfigId.Value

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
func (r resourcePitrConfig) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
