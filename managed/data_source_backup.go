/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	//"github.com/hashicorp/terraform-plugin-log/tflog"

	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type dataSourceBackupType struct{}

func (r dataSourceBackupType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "The data source to fetch the backup ID and other information about the most recent backup.",
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
			"most_recent": {
				Description: "Set to true to fetch the most recent backup.",
				Type:        types.BoolType,
				Optional:    true,
			},
			"timestamp": {
				Description: "The timestamp of the backup to be fetched. Format: '2022-07-08T00:06:01.890Z'.",
				Type:        types.StringType,
				Optional:    true,
			},
			"project_id": {
				Description: "The ID of the project this backup belongs to.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"backup_id": {
				Description: "The ID of the backup. Fetched from read.",
				Type:        types.StringType,
				Computed:    true,
			},
			"backup_description": {
				Description: "The description of the backup.",
				Type:        types.StringType,
				Optional:    true,
			},
			"retention_period_in_days": {
				Description: "The retention period of the backup.",
				Type:        types.Int64Type,
				Optional:    true,
			},
		},
	}, nil
}

func (r dataSourceBackupType) NewDataSource(_ context.Context, p tfsdk.Provider) (tfsdk.DataSource, diag.Diagnostics) {
	return dataSourceBackup{
		p: *(p.(*provider)),
	}, nil
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

	backup.AccountID.Value = accountId
	backup.ProjectID.Value = projectId
	backup.ClusterID.Value = clusterId

	if mostRecent {
		backup.BackupID.Value = backupsResp.Data[0].Info.GetId()
		backup.MostRecent.Value = mostRecent
		backup.Timestamp.Null = true
	} else {
		backupFound := false
		backupAvailable := true
		hasContinuationToken := backupsResp.Metadata.HasContinuationToken()
		for {
			for _, data := range backupsResp.Data {
				// Assumes the provided timestamp is in the format 2022-07-08T00:06:01.890Z
				createdOn := data.Info.Metadata.GetCreatedOn()
				// By default the backups are ordered based on the created time in descending
				// order. Ending the search if given timestamp is greater than current
				// timestamp in the loop
				if timestamp > createdOn {
					backupAvailable = false
					break
				}
				if timestamp == createdOn {
					backupFound = true
					backup.BackupID.Value = data.Info.GetId()
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

		backup.MostRecent.Null = true
		backup.Timestamp.Value = timestamp
	}

	return backup, true, ""
}

// Read backup datasource
func (r dataSourceBackup) Read(ctx context.Context, req tfsdk.ReadDataSourceRequest, resp *tfsdk.ReadDataSourceResponse) {

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

	if !config.AccountID.Null && !config.AccountID.Unknown {
		accountId = config.AccountID.Value
	} else {
		accountId, getAccountOK, message = getAccountId(ctx, apiClient)
		if !getAccountOK {
			resp.Diagnostics.AddError("Unable to get account ID", message)
			return
		}
	}

	if (!config.BackupID.Unknown && !config.BackupID.Null) || config.BackupID.Value != "" {
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

	clusterId := config.ClusterID.Value

	mostRecent := false
	if !config.MostRecent.Unknown && !config.MostRecent.Null && config.MostRecent.Value {
		mostRecent = true
	}

	timestampPresent := false
	timestamp := ""
	if (!config.Timestamp.Unknown && !config.Timestamp.Null) || config.Timestamp.Value != "" {
		timestampPresent = true
		timestamp = config.Timestamp.Value
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
