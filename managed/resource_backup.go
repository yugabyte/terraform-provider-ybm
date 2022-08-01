package managed

import (
	"context"
	"errors"
	"net/http/httputil"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	//"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceBackupType struct{}

func (r resourceBackupType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this backup belongs to.",
				Type:        types.StringType,
				Required:    true,
			},
			"cluster_id": {
				Description: "The ID of the cluster that needs to be backed up.",
				Type:        types.StringType,
				Required:    true,
			},
			"project_id": {
				Description: "The ID of the project this backup belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"backup_id": {
				Description: "The id of the backup. Filled automatically on creating a backup. Used to get a specific backup.",
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
				Description: "Set to true if the ID of the most recent backup is needed.",
				Type:        types.BoolType,
				Optional:    true,
			},
			"timestamp": {
				Description: "The timestamp of the backup that needs to be fetched",
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

	diags.Append(plan.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("account_id"), &backup.AccountID)...)
	diags.Append(plan.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("cluster_id"), &backup.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("backup_id"), &backup.BackupID)...)
	diags.Append(plan.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("backup_description"), &backup.BackupDescription)...)
	diags.Append(plan.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("retention_period_in_days"), &backup.RetentionPeriodInDays)...)

	return diags
}

// Create backup
func (r resourceBackup) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider hasn't been configured before apply, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan Backup
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(getBackupPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client

	accountId := plan.AccountID.Value

	if (!plan.BackupID.Unknown && !plan.BackupID.Null) || plan.BackupID.Value != "" {
		resp.Diagnostics.AddError(
			"Backup ID provided when creating a backup",
			"The backup_id field was provided even though a new backup is being created. Make sure this field is not in the provider on creation.",
		)
		return
	}

	projectId, getProjectOK, message := getProjectId(accountId, apiClient)
	if !getProjectOK {
		resp.Diagnostics.AddError("Could not get project ID", message)
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
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Could not create backup", string(b))
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
		return retry.RetryableError(errors.New("The backup creation didn't succeed yet"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not create backup", "Timed out waiting for backup creation to be successful.")
		return
	}

	backup, readOK, message := resourceBackupRead(accountId, projectId, backupId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Could not read the state of the backup", message)
		return
	}

	diags = resp.State.Set(ctx, &backup)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromBackupState(ctx context.Context, state tfsdk.State, backup *Backup) {
	state.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("account_id"), &backup.AccountID)
	state.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("project_id"), &backup.ProjectID)
	state.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("backup_id"), &backup.BackupID)
}

func resourceBackupRead(accountId string, projectId string, backupId string, apiClient *openapiclient.APIClient) (backup Backup, readOK bool, errorMessage string) {
	backupResp, response, err := apiClient.BackupApi.GetBackup(context.Background(), accountId, projectId, backupId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		return backup, false, string(b)
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
		resp.Diagnostics.AddError("Could not read the state of the backup", message)
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

	resp.Diagnostics.AddError("Could not update backup.", "Updating a backup is not supported yet. Please delete and recreate.")
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
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Could not delete the backup", string(b))
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
		return retry.RetryableError(errors.New("The backup deletion didn't succeed yet"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not delete backup", "Timed out waiting for backup deletion to be successful.")
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import backup
func (r resourceBackup) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, tftypes.NewAttributePath().WithAttributeName("id"), req, resp)
}
