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

type ignoreChangesModifier struct{}

func (m ignoreChangesModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {

	if req.StateValue.IsNull() {
		return
	}

	if !req.ConfigValue.IsNull() && !req.ConfigValue.Equal(req.StateValue) {
		resp.Diagnostics.AddWarning(
			"Value Change Ignored",
			fmt.Sprintf("An attempt to change the value of '%s' will be ignored as this field cannot be modified after creation.", req.Path.String()),
		)
	}

	resp.PlanValue = req.StateValue

}

func (m ignoreChangesModifier) Description(ctx context.Context) string {
	return "Ignores changes to this field after creation"
}

func (m ignoreChangesModifier) MarkdownDescription(ctx context.Context) string {
	return "Ignores changes to this field after creation"
}

func (r *resourceDrConfig) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The resource to create a Disaster Recovery configuration in YugabyteDB Managed.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this DR config belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this DR config belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"dr_config_id": schema.StringAttribute{
				Description: "The ID of the DR configuration.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"source_cluster_id": schema.StringAttribute{
				Description: "The ID of the source cluster for DR configuration.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					ignoreChangesModifier{},
				},
			},
			"target_cluster_id": schema.StringAttribute{
				Description: "The ID of the target cluster for DR configuration.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					ignoreChangesModifier{},
				},
			},
			"name": schema.StringAttribute{
				Description: "The name for DR configuration.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					ignoreChangesModifier{},
				},
			},
			"databases": schema.SetAttribute{
				Description: "List of databases to be included in DR configuration.",
				ElementType: types.StringType,
				Required:    true,
			},
		},
	}
}

func newResourceDrConfig() resource.Resource {
	return &resourceDrConfig{}
}

func (r *resourceDrConfig) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dr_config"
}

func (r *resourceDrConfig) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
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
func (r resourceDrConfig) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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
		databases = append(databases, db.ValueString())
	}

	sourceClusterId := plan.SourceClusterId.ValueString()
	targetClusterId := plan.TargetClusterId.ValueString()
	drName := plan.Name.ValueString()

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
	plan.AccountId = types.StringValue(accountId)
	plan.ProjectId = types.StringValue(projectId)
	plan.DrConfigId = types.StringValue(drConfigResp.Data.Info.Id)

	diags := resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read DR configuration
func (r resourceDrConfig) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DrConfig
	getDrConfigState(ctx, req.State, &state)

	apiClient := r.p.client
	accountId := state.AccountId.ValueString()
	projectId := state.ProjectId.ValueString()
	sourceClusterId := state.SourceClusterId.ValueString()
	drConfigId := state.DrConfigId.ValueString()

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
	state.SourceClusterId = types.StringValue(drConfigResp.Data.Info.SourceClusterId)
	state.TargetClusterId = types.StringValue(drConfigResp.Data.Spec.TargetClusterId)

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
		databases = append(databases, types.StringValue(database))
	}
	state.Databases = databases

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Edit DR configuration
func (r resourceDrConfig) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
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
	if plan.Name.ValueString() != state.Name.ValueString() ||
		plan.SourceClusterId.ValueString() != state.SourceClusterId.ValueString() ||
		plan.TargetClusterId.ValueString() != state.TargetClusterId.ValueString() {
		resp.Diagnostics.AddError(
			"Invalid edit to DR configuration",
			"Only the databases field can be modified in DR configurations. Other fields cannot be changed.",
		)
		return
	}

	apiClient := r.p.client
	accountId := state.AccountId.ValueString()
	projectId := state.ProjectId.ValueString()
	sourceClusterId := state.SourceClusterId.ValueString()
	drConfigId := state.DrConfigId.ValueString()

	// Convert planned databases from []types.String to []string
	databases := []string{}
	for _, db := range plan.Databases {
		databases = append(databases, db.ValueString())
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
func (r resourceDrConfig) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DrConfig
	getDrConfigState(ctx, req.State, &state)
	accountId := state.AccountId.ValueString()
	projectId := state.ProjectId.ValueString()
	clusterId := state.SourceClusterId.ValueString()
	drConfigId := state.DrConfigId.ValueString()

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
func (r resourceDrConfig) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
