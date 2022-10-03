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

	//"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceBackupType struct{}

func (r resourceBackupType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a manual backup of tables in a particular cluster. 
		Ensure that the cluster for which the backup is being taken has data.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this backup belongs to. To be provided if there are multiple accounts associated with the user.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"cluster_id": {
				Description: "The ID of the cluster to be backed up.",
				Type:        types.StringType,
				Required:    true,
			},
			"project_id": {
				Description: "The ID of the project this backup belongs to.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"backup_id": {
				Description: "The ID of the backup. Created automatically when the backup is created. Used to get a specific backup.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"backup_description": {
				Description: "The description of the backup.",
				Type:        types.StringType,
				Required:    true,
			},
			"retention_period_in_days": {
				Description: "The retention period of the backup.",
				Type:        types.Int64Type,
				Required:    true,
			},
			"most_recent": {
				Description: "Set to true to fetch the most recent backup.",
				Type:        types.BoolType,
				Optional:    true,
			},
			"timestamp": {
				Description: "The timestamp of the backup to be fetched",
				Type:        types.StringType,
				Optional:    true,
			},
		},
	}, nil
}

func (r resourceBackupType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceBackup{
		p: *(p.(*provider)),
	}, nil
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

	diags.Append(plan.GetAttribute(ctx, path.Root("account_id"), &backup.AccountID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &backup.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("backup_id"), &backup.BackupID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("backup_description"), &backup.BackupDescription)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("retention_period_in_days"), &backup.RetentionPeriodInDays)...)

	return diags
}

// Create backup
func (r resourceBackup) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

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

	if !plan.AccountID.Null && !plan.AccountID.Unknown {
		accountId = plan.AccountID.Value
	} else {
		accountId, getAccountOK, message = getAccountId(ctx, apiClient)
		if !getAccountOK {
			resp.Diagnostics.AddError("Unable to get account ID", message)
			return
		}
	}

	if (!plan.BackupID.Unknown && !plan.BackupID.Null) || plan.BackupID.Value != "" {
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

	clusterId := plan.ClusterID.Value
	backupDescription := plan.BackupDescription.Value
	backupRetentionPeriodInDays := int32(plan.RetentionPeriodInDays.Value)

	backupSpec := *openapiclient.NewBackupSpec(clusterId)
	backupSpec.Description = &backupDescription
	backupSpec.RetentionPeriodInDays = &backupRetentionPeriodInDays

	backupResp, response, err := apiClient.BackupApi.CreateBackup(context.Background(), accountId, projectId).BackupSpec(backupSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create backup ", errMsg)
		return
	}
	backupId := *(backupResp.Data.Info.Id)

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(600*time.Second, retryPolicy)
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

	backup.AccountID.Value = accountId
	backup.ProjectID.Value = projectId
	backup.BackupID.Value = backupId

	backup.ClusterID.Value = backupResp.Data.Spec.ClusterId
	backup.BackupDescription.Value = *(backupResp.Data.Spec.Description)
	backup.RetentionPeriodInDays.Value = int64(*backupResp.Data.Spec.RetentionPeriodInDays)
	backup.MostRecent.Null = true
	backup.Timestamp.Null = true
	return backup, true, ""
}

// Read backup
func (r resourceBackup) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state Backup
	getIDsFromBackupState(ctx, req.State, &state)

	backup, readOK, message := resourceBackupRead(state.AccountID.Value, state.ProjectID.Value, state.BackupID.Value, r.p.client)
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
func (r resourceBackup) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {

	resp.Diagnostics.AddError("Unable to update backup.", "Updating backups is not currently supported. Delete and recreate the provider.")
	return
}

// Delete backup
func (r resourceBackup) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state Backup
	getIDsFromBackupState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	backupId := state.BackupID.Value

	apiClient := r.p.client

	response, err := apiClient.BackupApi.DeleteBackup(context.Background(), accountId, projectId, backupId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Could not delete the backup", errMsg)
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(300*time.Second, retryPolicy)
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
func (r resourceBackup) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
