/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	//"github.com/hashicorp/terraform-plugin-log/tflog"

	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *dataSourceBackup) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The data source to fetch the backup ID and other information about the most recent backup.",
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this backup belongs to.",
				Computed:    true,
			},
			"cluster_id": schema.StringAttribute{
				Description: "The ID of the cluster to be backed up.",
				Required:    true,
			},
			"most_recent": schema.BoolAttribute{
				Description: "Set to true to fetch the most recent backup.",
				Optional:    true,
			},
			"timestamp": schema.StringAttribute{
				Description: "The timestamp of the backup to be fetched. Format: '2022-07-08T00:06:01.890Z'.",
				Optional:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this backup belongs to.",
				Computed:    true,
			},
			"backup_id": schema.StringAttribute{
				Description: "The ID of the backup. Fetched from read.",
				Computed:    true,
			},
			"backup_description": schema.StringAttribute{
				Description: "The description of the backup.",
				Optional:    true,
			},
			"retention_period_in_days": schema.Int64Attribute{
				Description: "The retention period of the backup.",
				Optional:    true,
			},
			"use_roles": schema.BoolAttribute{
				Description: "Backup global YSQL roles. Defaults to false.",
				Computed:    true,
			},
		},
	}
}

func newDataSourceBackup() datasource.DataSource {
	return &dataSourceBackup{}
}

func (r *dataSourceBackup) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backup"
}

func (r *dataSourceBackup) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type dataSourceBackup struct {
	p provider
}

func dataSourceBackupRead(ctx context.Context, accountId string, projectId string, clusterId string, mostRecent bool, timestamp string, apiClient *openapiclient.APIClient) (backup Backup, readOK bool, errorMessage string) {

	state := "SUCCEEDED"
	backupsResp, response, err := apiClient.BackupApi.ListBackups(ctx, accountId, projectId).ClusterId(clusterId).State(state).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return backup, false, errMsg
	}
	backupCount := len(backupsResp.Data)
	if backupCount == 0 {
		return backup, false, "No backups available."
	}

	backup.AccountID = types.StringValue(accountId)
	backup.ProjectID = types.StringValue(projectId)
	backup.ClusterID = types.StringValue(clusterId)
	backup.BackupDescription = types.StringValue("")
	backup.RetentionPeriodInDays = types.Int64Value(0)
	backup.UseRoles = types.BoolValue(false)

	if mostRecent {
		backup.BackupID = types.StringValue(backupsResp.Data[0].Info.GetId())
		backup.MostRecent = types.BoolValue(mostRecent)
		backup.Timestamp = types.StringNull()
	} else {
		backupFound := false
		backupAvailable := true
		hasContinuationToken := backupsResp.Metadata.HasContinuationToken()
		for {
			for _, data := range backupsResp.Data {
				// Assumes the provided timestamp is in the format 2022-07-08T00:06:01.890Z
				createdOn := data.Info.Metadata.Get().GetCreatedOn()
				// By default the backups are ordered based on the created time in descending
				// order. Ending the search if given timestamp is greater than current
				// timestamp in the loop
				if timestamp > createdOn {
					backupAvailable = false
					break
				}
				if timestamp == createdOn {
					backupFound = true
					backup.BackupID = types.StringValue(data.Info.GetId())
					break
				}
			}
			if backupFound || !backupAvailable || !hasContinuationToken {
				break
			}
			continuationToken := backupsResp.Metadata.GetContinuationToken()
			backupsResp, response, err = apiClient.BackupApi.ListBackups(ctx, accountId, projectId).ClusterId(clusterId).State(state).ContinuationToken(continuationToken).Execute()
			if err != nil {
				errMsg := getErrorMessage(response, err)
				return backup, false, errMsg
			}
		}
		if !backupFound {
			return backup, false, "Backup with given timestamp not found."
		}

		backup.MostRecent = types.BoolNull()
		backup.Timestamp = types.StringValue(timestamp)
	}

	return backup, true, ""
}

// Read backup datasource
func (r dataSourceBackup) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var config Backup
	var accountId, message string
	var getAccountOK bool
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	if (!config.BackupID.IsUnknown() && !config.BackupID.IsNull()) || config.BackupID.ValueString() != "" {
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

	clusterId := config.ClusterID.ValueString()

	mostRecent := false
	if !config.MostRecent.IsUnknown() && !config.MostRecent.IsNull() && config.MostRecent.ValueBool() {
		mostRecent = true
	}

	timestampPresent := false
	timestamp := ""
	if (!config.Timestamp.IsUnknown() && !config.Timestamp.IsNull()) || config.Timestamp.ValueString() != "" {
		timestampPresent = true
		timestamp = config.Timestamp.ValueString()
	}

	// Exactly one parameter amongst most_recent and timestamp must be present
	// Simulating XOR by comparing boolean values
	if mostRecent == timestampPresent {
		resp.Diagnostics.AddError(
			"Specify most_recent or a timestamp",
			"To choose a backup, use either most_recent or provide a timestamp. Don't provide both.",
		)
		return
	}

	backup, readOK, message := dataSourceBackupRead(ctx, accountId, projectId, clusterId, mostRecent, timestamp, r.p.client)

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
