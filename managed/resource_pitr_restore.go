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

type resourcePitrRestoreType struct{}

func (r resourcePitrRestoreType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to restore a namespace via PITR Config in YugabyteDB Aeon.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"project_id": {
				Description: "The ID of the project.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"cluster_id": {
				Description: "The ID of the cluster.",
				Type:        types.StringType,
				Required:    true,
			},
			"pitr_config_id": {
				Description: "The ID of the PITR config to be used to perform the restore.",
				Type:        types.StringType,
				Required:    true,
			},
			"pitr_restore_id": {
				Description: "The ID of the restore op via PITR Config.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"restore_at_millis": {
				Description: "The time in UNIX millis to restore to via PITR Config.",
				Type:        types.Int64Type,
				Required:    true,
			},
			"state": {
				Description: "The status of the restoration via PITR config.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
		},
	}, nil
}

func (r resourcePitrRestoreType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourcePitrRestore{
		p: *(p.(*provider)),
	}, nil
}

type resourcePitrRestore struct {
	p provider
}

func getPitrRestorePlan(ctx context.Context, plan tfsdk.Plan, pitrRestore *PitrRestore) diag.Diagnostics {
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &pitrRestore.ClusterId)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("pitr_config_id"), &pitrRestore.PitrConfigId)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("restore_at_millis"), &pitrRestore.RestoreAtMillis)...)

	return diags
}

func getPitrRestoreState(ctx context.Context, state tfsdk.State, pitrRestore *PitrRestore) {
	state.GetAttribute(ctx, path.Root("account_id"), &pitrRestore.AccountId)
	state.GetAttribute(ctx, path.Root("project_id"), &pitrRestore.ProjectId)
	state.GetAttribute(ctx, path.Root("cluster_id"), &pitrRestore.ClusterId)
	state.GetAttribute(ctx, path.Root("pitr_config_id"), &pitrRestore.PitrConfigId)
	state.GetAttribute(ctx, path.Root("pitr_restore_id"), &pitrRestore.PitrRestoreId)
	state.GetAttribute(ctx, path.Root("restore_at_millis"), &pitrRestore.RestoreAtMillis)
	state.GetAttribute(ctx, path.Root("state"), &pitrRestore.State)
}

// Restore via PITR Config
func (r resourcePitrRestore) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan PitrRestore
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getPitrRestorePlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the PITR Restore")
		return
	}

	if (!plan.PitrRestoreId.Unknown && !plan.PitrRestoreId.Null) || plan.PitrRestoreId.Value != "" {
		resp.Diagnostics.AddError(
			"PITR Restore ID provided for new restore via PITR Config",
			"The pitr_restore_id was provided even though a new restore via PITR Config is being requested. Do not include this field in the provider when restoring via PITR Config.",
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
	pitrConfigId := plan.PitrConfigId.Value

	pitrRestoreResp, response, err := apiClient.ClusterApi.RestoreDatabaseViaPitr(context.Background(), accountId, projectId, clusterId, pitrConfigId).DatabaseRestoreViaPitrSpec(*openapiclient.NewDatabaseRestoreViaPitrSpec(plan.RestoreAtMillis.Value)).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to restore via PITR Config ", errMsg)
		return
	}

	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_RESTORE_DB_PITR, apiClient, ctx)

		tflog.Info(ctx, "Restoration via PITR config in progress, state: "+asState)

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
		return retry.RetryableError(errors.New("Restoration via PITR config in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for restoration via PITR config to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "Restoration via PITR config failed"
		}
		resp.Diagnostics.AddError("Unable to restore via PITR config:", msg)
		return
	}

	// Set the computed fields
	plan.AccountId = types.String{Value: accountId}
	plan.ProjectId = types.String{Value: projectId}
	plan.PitrRestoreId = types.String{Value: pitrRestoreResp.Data.Info.GetId()}
	plan.State = types.String{Value: *&pitrRestoreResp.Data.Info.State}

	diags := resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read PITR restore
func (r resourcePitrRestore) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state PitrRestore
	getPitrRestoreState(ctx, req.State, &state)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update PITR restore
func (r resourcePitrRestore) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	resp.Diagnostics.AddError("Unable to update PITR restore.", "Updating PITR restores is not supported.")
	return
}

// Delete PITR restore
func (r resourcePitrRestore) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	resp.Diagnostics.AddError("Unable to delete PITR restore.", "Deleting PITR restores is not supported.")
	return
}

// Import PITR restore
func (r resourcePitrRestore) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
