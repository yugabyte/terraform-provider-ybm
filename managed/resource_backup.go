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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	//"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceBackup) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The resource to create a manual backup of tables in a particular cluster. 
		Ensure that the cluster for which the backup is being taken has data.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this backup belongs to.",
				Computed:    true,
			},
			"cluster_id": schema.StringAttribute{
				Description: "The ID of the cluster to be backed up.",
				Required:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this backup belongs to.",
				Computed:    true,
			},
			"backup_id": schema.StringAttribute{
				Description: "The ID of the backup. Created automatically when the backup is created. Used to get a specific backup.",
				Computed:    true,
				Optional:    true,
			},
			"backup_description": schema.StringAttribute{
				Description: "The description of the backup.",
				Required:    true,
			},
			"retention_period_in_days": schema.Int64Attribute{
				Description: "The retention period of the backup.",
				Required:    true,
			},
			"most_recent": schema.BoolAttribute{
				Description: "Set to true to fetch the most recent backup.",
				Optional:    true,
			},
			"timestamp": schema.StringAttribute{
				Description: "The timestamp of the backup to be fetched",
				Optional:    true,
			},
			"use_roles": schema.BoolAttribute{
				Description: "Backup global YSQL roles. Defaults to false.",
				Optional:    true,
				Computed:    true,
			},
		},
	}
}

func newResourceBackup() resource.Resource {
	return &resourceBackup{}
}

func (r *resourceBackup) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backup"
}

func (r *resourceBackup) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceBackup struct {
	p provider
}

func getBackupPlan(ctx context.Context, plan tfsdk.Plan, backup *Backup) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &backup)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &backup.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("backup_id"), &backup.BackupID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("backup_description"), &backup.BackupDescription)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("retention_period_in_days"), &backup.RetentionPeriodInDays)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("use_roles"), &backup.UseRoles)...)

	return diags
}

// Create backup
func (r resourceBackup) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan Backup
	var accountId, message string
	var getAccountOK bool
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(getBackupPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	if (!plan.BackupID.IsUnknown() && !plan.BackupID.IsNull()) || plan.BackupID.ValueString() != "" {
		resp.Diagnostics.AddError(
			"Backup ID provided for new backup",
			"The backup_id was provided even though a new backup is being created. Do not include this field in the provider when creating a backup.",
		)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get the project ID ", message)
		return
	}

	clusterId := plan.ClusterID.ValueString()
	backupDescription := plan.BackupDescription.ValueString()
	backupRetentionPeriodInDays := int32(plan.RetentionPeriodInDays.ValueInt64())

	backupSpec := *openapiclient.NewBackupSpec(clusterId)
	backupSpec.SetDescription(backupDescription)
	backupSpec.RetentionPeriodInDays = &backupRetentionPeriodInDays

	if !plan.UseRoles.IsNull() && !plan.UseRoles.IsUnknown() {
		backupSpec.SetUseRoles(plan.UseRoles.ValueBool())
	}

	backupResp, response, err := apiClient.BackupApi.CreateBackup(context.Background(), accountId, projectId).BackupSpec(backupSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create backup ", errMsg)
		return
	}
	backupId := *(backupResp.Data.Info.Id)

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(120*time.Minute, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		backupResp, _, err := apiClient.BackupApi.GetBackup(context.Background(), accountId, projectId, backupId).Execute()
		if err == nil {
			if *(backupResp.Data.Info.State) == "SUCCEEDED" {
				return nil
			}
		}
		return retry.RetryableError(errors.New("The backup hasn't finished."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create backup", "The operation timed out waiting for the backup to complete.")
		return
	}

	backup, readOK, message := resourceBackupRead(accountId, projectId, backupId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the backup ", message)
		return
	}

	diags = resp.State.Set(ctx, &backup)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromBackupState(ctx context.Context, state tfsdk.State, backup *Backup) {
	state.GetAttribute(ctx, path.Root("account_id"), &backup.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &backup.ProjectID)
	state.GetAttribute(ctx, path.Root("backup_id"), &backup.BackupID)
}

func resourceBackupRead(accountId string, projectId string, backupId string, apiClient *openapiclient.APIClient) (backup Backup, readOK bool, errorMessage string) {
	backupResp, response, err := apiClient.BackupApi.GetBackup(context.Background(), accountId, projectId, backupId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return backup, false, errMsg
	}

	backup.AccountID = types.StringValue(accountId)
	backup.ProjectID = types.StringValue(projectId)
	backup.BackupID = types.StringValue(backupId)

	backup.ClusterID = types.StringValue(backupResp.Data.Spec.ClusterId)
	backup.BackupDescription = types.StringValue(*(backupResp.Data.Spec.Description.Get()))
	backup.RetentionPeriodInDays = types.Int64Value(int64(*backupResp.Data.Spec.RetentionPeriodInDays))
	backup.MostRecent = types.BoolNull()
	backup.Timestamp = types.StringNull()

	// Read use_roles from response (defaults to false if not present)
	if backupResp.Data.Spec.HasUseRoles() {
		backup.UseRoles = types.BoolValue(backupResp.Data.Spec.GetUseRoles())
	} else {
		backup.UseRoles = types.BoolValue(false)
	}

	return backup, true, ""
}

// Read backup
func (r resourceBackup) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state Backup
	getIDsFromBackupState(ctx, req.State, &state)

	backup, readOK, message := resourceBackupRead(state.AccountID.ValueString(), state.ProjectID.ValueString(), state.BackupID.ValueString(), r.p.client)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the backup ", message)
		return
	}

	diags := resp.State.Set(ctx, &backup)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update backup
func (r resourceBackup) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	resp.Diagnostics.AddError("Unable to update backup.", "Updating backups is not currently supported. Delete and recreate the provider.")
	return
}

// Delete backup
func (r resourceBackup) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state Backup
	getIDsFromBackupState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	backupId := state.BackupID.ValueString()

	apiClient := r.p.client

	response, err := apiClient.BackupApi.DeleteBackup(context.Background(), accountId, projectId, backupId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Could not delete the backup", errMsg)
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(120*time.Minute, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		_, resp, err := apiClient.BackupApi.GetBackup(context.Background(), accountId, projectId, backupId).Execute()
		if err != nil {
			if resp.StatusCode == 404 {
				return nil
			}
		}
		return retry.RetryableError(errors.New("Backup deletion hasn't finished."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to delete backup", "The operation timed out waiting for the backup deletion to complete.")
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import backup
func (r resourceBackup) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
