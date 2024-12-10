/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceDrConfigType struct{}

type ignoreChangesModifier struct{}

func (m ignoreChangesModifier) Modify(ctx context.Context, req tfsdk.ModifyAttributePlanRequest, resp *tfsdk.ModifyAttributePlanResponse) {

	if req.AttributeState.IsNull() {
		return
	}

	if !req.AttributeConfig.IsNull() && !req.AttributeConfig.Equal(req.AttributeState) {
		resp.Diagnostics.AddWarning(
			"Value Change Ignored",
			fmt.Sprintf("An attempt to change the value of '%s' will be ignored as this field cannot be modified after creation.", req.AttributePath.String()),
		)
	}

	resp.AttributePlan = req.AttributeState

}

func (m ignoreChangesModifier) Description(ctx context.Context) string {
	return "Ignores changes to this field after creation"
}

func (m ignoreChangesModifier) MarkdownDescription(ctx context.Context) string {
	return "Ignores changes to this field after creation"
}

func (r resourceDrConfigType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a Disaster Recovery configuration in YugabyteDB Managed.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this DR config belongs to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"project_id": {
				Description: "The ID of the project this DR config belongs to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"dr_config_id": {
				Description: "The ID of the DR configuration.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"source_cluster_id": {
				Description: "The ID of the source cluster for DR configuration.",
				Type:        types.StringType,
				Required:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					ignoreChangesModifier{},
				},
			},
			"target_cluster_id": {
				Description: "The ID of the target cluster for DR configuration.",
				Type:        types.StringType,
				Required:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					ignoreChangesModifier{},
				},
			},
			"name": {
				Description: "The name for DR configuration.",
				Type:        types.StringType,
				Required:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					ignoreChangesModifier{},
				},
			},
			"databases": {
				Description: "List of databases to be included in DR configuration.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Required: true,
			},
		},
	}, nil
}

func (r resourceDrConfigType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceDrConfig{
		p: *(p.(*provider)),
	}, nil
}

type resourceDrConfig struct {
	p provider
}

func getDrConfigPlan(ctx context.Context, plan tfsdk.Plan, drConfig *DrConfig) diag.Diagnostics {
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("source_cluster_id"), &drConfig.SourceClusterId)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("target_cluster_id"), &drConfig.TargetClusterId)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("databases"), &drConfig.Databases)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("name"), &drConfig.Name)...)

	return diags
}

func getDrConfigState(ctx context.Context, state tfsdk.State, drConfig *DrConfig) {
	state.GetAttribute(ctx, path.Root("account_id"), &drConfig.AccountId)
	state.GetAttribute(ctx, path.Root("project_id"), &drConfig.ProjectId)
	state.GetAttribute(ctx, path.Root("dr_config_id"), &drConfig.DrConfigId)
	state.GetAttribute(ctx, path.Root("name"), &drConfig.Name)
	state.GetAttribute(ctx, path.Root("source_cluster_id"), &drConfig.SourceClusterId)
	state.GetAttribute(ctx, path.Root("target_cluster_id"), &drConfig.TargetClusterId)
	state.GetAttribute(ctx, path.Root("databases"), &drConfig.Databases)
}

// Create DR configuration
func (r resourceDrConfig) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan DrConfig
	resp.Diagnostics.Append(getDrConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the DR config")
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
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}

	// Convert databases from []types.String to []string
	databases := []string{}
	for _, db := range plan.Databases {
		databases = append(databases, db.Value)
	}

	sourceClusterId := plan.SourceClusterId.Value
	targetClusterId := plan.TargetClusterId.Value
	drName := plan.Name.Value

	namespacesResp, response, err := apiClient.ClusterApi.GetClusterNamespaces(ctx, accountId, projectId, sourceClusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create DR configuration", errMsg)
		return
	}

	dbNameToIdMap := map[string]string{}
	for _, namespace := range namespacesResp.Data {
		dbNameToIdMap[namespace.GetName()] = namespace.GetId()
	}
	databaseIds := []string{}
	for _, database := range databases {
		if databaseId, exists := dbNameToIdMap[database]; exists {
			databaseIds = append(databaseIds, databaseId)
		} else {
			msg := "The database " + database + " doesn't exist"
			resp.Diagnostics.AddError("Unable to create DR configuration", msg)
		}
	}

	createDrRequest := openapiclient.NewCreateXClusterDrRequest(*openapiclient.NewXClusterDrSpec(drName, targetClusterId, databaseIds))

	drConfigResp, response, err := apiClient.XclusterDrApi.CreateXClusterDr(ctx, accountId, projectId, sourceClusterId).CreateXClusterDrRequest(*createDrRequest).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create DR configuration", errMsg)
		return
	}

	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, sourceClusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_CREATE_DR, apiClient, ctx)

		tflog.Info(ctx, "DR config creation operation in progress, state: "+asState)

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
		return retry.RetryableError(errors.New("DR config creation operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for DR config creation to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "DR config creation operation failed"
		}
		resp.Diagnostics.AddError("Unable to create DR config:", msg)
		return
	}

	// Set the computed fields
	plan.AccountId = types.String{Value: accountId}
	plan.ProjectId = types.String{Value: projectId}
	plan.DrConfigId = types.String{Value: drConfigResp.Data.Info.Id}

	diags := resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read DR configuration
func (r resourceDrConfig) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state DrConfig
	getDrConfigState(ctx, req.State, &state)

	apiClient := r.p.client
	accountId := state.AccountId.Value
	projectId := state.ProjectId.Value
	sourceClusterId := state.SourceClusterId.Value
	drConfigId := state.DrConfigId.Value

	drConfigResp, response, err := apiClient.XclusterDrApi.GetXClusterDr(ctx, accountId, projectId, sourceClusterId, drConfigId).Execute()
	if err != nil {
		if response != nil && response.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to read DR configuration", errMsg)
		return
	}

	// Update state with the current values
	state.SourceClusterId = types.String{Value: drConfigResp.Data.Info.SourceClusterId}
	state.TargetClusterId = types.String{Value: drConfigResp.Data.Spec.TargetClusterId}

	namespacesResp, response, err := apiClient.ClusterApi.GetClusterNamespaces(ctx, accountId, projectId, sourceClusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create DR configuration", errMsg)
		return
	}

	dbIdToNameMap := map[string]string{}
	for _, namespace := range namespacesResp.Data {
		dbIdToNameMap[namespace.GetId()] = namespace.GetName()
	}

	databases := []types.String{}
	for _, dbId := range drConfigResp.Data.Spec.DatabaseIds {
		database := dbIdToNameMap[dbId]
		databases = append(databases, types.String{Value: database})
	}
	state.Databases = databases

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Edit DR configuration
func (r resourceDrConfig) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan DrConfig
	var state DrConfig

	// Get current state
	getDrConfigState(ctx, req.State, &state)

	// Get planned changes
	resp.Diagnostics.Append(getDrConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the DR config edit")
		return
	}

	// Verify that only databases field is being changed
	if plan.Name.Value != state.Name.Value ||
		plan.SourceClusterId.Value != state.SourceClusterId.Value ||
		plan.TargetClusterId.Value != state.TargetClusterId.Value {
		resp.Diagnostics.AddError(
			"Invalid edit to DR configuration",
			"Only the databases field can be modified in DR configurations. Other fields cannot be changed.",
		)
		return
	}

	apiClient := r.p.client
	accountId := state.AccountId.Value
	projectId := state.ProjectId.Value
	sourceClusterId := state.SourceClusterId.Value
	drConfigId := state.DrConfigId.Value

	// Convert planned databases from []types.String to []string
	databases := []string{}
	for _, db := range plan.Databases {
		databases = append(databases, db.Value)
	}

	// Get database IDs for the new database list
	namespacesResp, response, err := apiClient.ClusterApi.GetClusterNamespaces(ctx, accountId, projectId, sourceClusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to edit DR configuration", errMsg)
		return
	}

	dbNameToIdMap := map[string]string{}
	for _, namespace := range namespacesResp.Data {
		dbNameToIdMap[namespace.GetName()] = namespace.GetId()
	}

	databaseIds := []string{}
	for _, database := range databases {
		if databaseId, exists := dbNameToIdMap[database]; exists {
			databaseIds = append(databaseIds, databaseId)
		} else {
			msg := "The database " + database + " doesn't exist"
			resp.Diagnostics.AddError("Unable to edit DR configuration", msg)
			return
		}
	}

	// Create edit request with new database IDs
	editDrRequest := openapiclient.NewEditXClusterDrRequest(*openapiclient.NewEditXClusterDrSpec(databaseIds))

	_, response, err = apiClient.XclusterDrApi.EditXClusterDr(ctx, accountId, projectId, sourceClusterId, drConfigId).EditXClusterDrRequest(*editDrRequest).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to edit DR configuration", errMsg)
		return
	}

	// Wait for  to complete
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, sourceClusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_EDIT_DR, apiClient, ctx)

		tflog.Info(ctx, "DR config edit operation in progress, state: "+asState)

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
		return retry.RetryableError(errors.New("DR config edit operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for DR config edit to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "DR config edit operation failed"
		}
		resp.Diagnostics.AddError("Unable to edit DR config:", msg)
		return
	}

	// Set state to planned new state
	plan.AccountId = state.AccountId
	plan.ProjectId = state.ProjectId
	plan.DrConfigId = state.DrConfigId

	diags := resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete DR configuration
func (r resourceDrConfig) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state DrConfig
	getDrConfigState(ctx, req.State, &state)
	accountId := state.AccountId.Value
	projectId := state.ProjectId.Value
	clusterId := state.SourceClusterId.Value
	drConfigId := state.DrConfigId.Value

	apiClient := r.p.client

	_, err := apiClient.XclusterDrApi.DeleteXClusterDr(ctx, accountId, projectId, clusterId, drConfigId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete DR configuration", GetApiErrorDetails(err))
		return
	}

	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_DELETE_DR, apiClient, ctx)

		tflog.Info(ctx, "DR config delete operation in progress, state: "+asState)

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
		return retry.RetryableError(errors.New("DR config deletion operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for DR config deletion to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "DR config deletion operation failed"
		}
		resp.Diagnostics.AddError("Unable to delete DR config:", msg)
		return
	}

	resp.State.RemoveResource(ctx)
}

// Import DR configuration
func (r resourceDrConfig) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
