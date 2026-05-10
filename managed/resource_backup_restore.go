/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceBackupRestoreType struct{}

func (r resourceBackupRestoreType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `Restore a backup to a cluster. All attributes are immutable; changing any attribute triggers destroy and create (a new restore). Use ysql_databases and ycql_keyspaces for selective restore; omit to restore all databases/keyspaces.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project.",
				Type:        types.StringType,
				Computed:    true,
			},
			"restore_id": {
				Description: "The restore operation ID.",
				Type:        types.StringType,
				Computed:    true,
			},
			"backup_id": {
				Description: "The ID of the backup to restore.",
				Type:        types.StringType,
				Required:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"target_cluster_id": {
				Description: "The ID of the cluster to restore the backup onto.",
				Type:        types.StringType,
				Required:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"use_roles": {
				Description: "Restore global YSQL roles. Defaults to false.",
				Type:        types.BoolType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"ysql_databases": {
				Description: "List of YSQL databases to restore. If empty or omitted, all YSQL databases are restored.",
				Type: types.ListType{
					ElemType: types.StringType,
				},
				Optional:   true,
				Validators: listOfNonEmptyStringValidators(),
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"ycql_keyspaces": {
				Description: "List of YCQL keyspaces to restore. If empty or omitted, all YCQL keyspaces are restored.",
				Type: types.ListType{
					ElemType: types.StringType,
				},
				Optional:   true,
				Validators: listOfNonEmptyStringValidators(),
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"ysql_databases_rename": {
				Description: "List of YSQL database renames (backup_database -> restore_database).",
				Optional:    true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"backup_database": {
						Description: "YSQL database name in the backup.",
						Type:        types.StringType,
						Required:    true,
						Validators:  nonEmptyStringValidators(),
					},
					"restore_database": {
						Description: "YSQL database name to use on the restored cluster.",
						Type:        types.StringType,
						Required:    true,
						Validators:  nonEmptyStringValidators(),
					},
				}),
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"ycql_keyspaces_rename": {
				Description: "List of YCQL keyspace renames (backup_database -> restore_database).",
				Optional:    true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"backup_database": {
						Description: "YCQL keyspace name in the backup.",
						Type:        types.StringType,
						Required:    true,
						Validators:  nonEmptyStringValidators(),
					},
					"restore_database": {
						Description: "YCQL keyspace name to use on the restored cluster.",
						Type:        types.StringType,
						Required:    true,
						Validators:  nonEmptyStringValidators(),
					},
				}),
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
		},
	}, nil
}

func (r resourceBackupRestoreType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceBackupRestore{
		p: *(p.(*provider)),
	}, nil
}

type resourceBackupRestore struct {
	p provider
}

func getBackupRestorePlan(ctx context.Context, plan tfsdk.Plan, br *BackupRestore) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("backup_id"), &br.BackupID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("target_cluster_id"), &br.TargetClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("use_roles"), &br.UseRoles)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("ysql_databases"), &br.YSQLDatabases)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("ycql_keyspaces"), &br.YCQLKeyspaces)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("ysql_databases_rename"), &br.YSQLDatabasesRename)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("ycql_keyspaces_rename"), &br.YCQLKeyspacesRename)...)
	return diags
}

func buildRestoreSpec(plan *BackupRestore) *openapiclient.RestoreSpec {
	restoreSpec := openapiclient.NewRestoreSpec()
	restoreSpec.SetBackupId(plan.BackupID.Value)
	restoreSpec.SetClusterId(plan.TargetClusterID.Value)
	restoreSpec.SetUseRoles(plan.UseRoles.Value)

	hasYSQL := len(plan.YSQLDatabases) > 0
	hasYCQL := len(plan.YCQLKeyspaces) > 0
	if hasYSQL || hasYCQL {
		sel := openapiclient.NewSelectiveRestoreSpec()
		if hasYSQL {
			ysql := make([]string, 0, len(plan.YSQLDatabases))
			for _, s := range plan.YSQLDatabases {
				if !s.Null && !s.Unknown {
					ysql = append(ysql, s.Value)
				}
			}
			sel.SetYsqlKeyspaces(ysql)
		}
		if hasYCQL {
			ycql := make([]string, 0, len(plan.YCQLKeyspaces))
			for _, s := range plan.YCQLKeyspaces {
				if !s.Null && !s.Unknown {
					ycql = append(ycql, s.Value)
				}
			}
			sel.SetYcqlKeyspaces(ycql)
		}
		restoreSpec.SetSelectiveRestoreSpec(*sel)
	}

	hasYSQLRename := len(plan.YSQLDatabasesRename) > 0
	hasYCQLRename := len(plan.YCQLKeyspacesRename) > 0
	if hasYSQLRename || hasYCQLRename {
		ksUpdate := openapiclient.NewKeyspaceUpdateSpec()
		if hasYSQLRename {
			ysqlList := make([]openapiclient.KeyspaceRenameSpec, 0, len(plan.YSQLDatabasesRename))
			for _, b := range plan.YSQLDatabasesRename {
				ysqlList = append(ysqlList, *openapiclient.NewKeyspaceRenameSpec(b.BackupDatabase.Value, b.RestoreDatabase.Value))
			}
			ksUpdate.SetYsql(ysqlList)
		}
		if hasYCQLRename {
			ycqlList := make([]openapiclient.KeyspaceRenameSpec, 0, len(plan.YCQLKeyspacesRename))
			for _, b := range plan.YCQLKeyspacesRename {
				ycqlList = append(ycqlList, *openapiclient.NewKeyspaceRenameSpec(b.BackupDatabase.Value, b.RestoreDatabase.Value))
			}
			ksUpdate.SetYcql(ycqlList)
		}
		restoreSpec.SetKeyspaceUpdateSpec(*ksUpdate)
	}

	return restoreSpec
}

func (r resourceBackupRestore) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider was not configured before being applied.",
		)
		return
	}

	var plan BackupRestore
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(getBackupRestorePlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
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

	// Default use_roles to false if not set
	if plan.UseRoles.Null || plan.UseRoles.Unknown {
		plan.UseRoles = types.Bool{Value: false}
	}

	restoreSpec := buildRestoreSpec(&plan)
	tflog.Debug(ctx, "Restoring backup to cluster", map[string]interface{}{
		"backup_id":         plan.BackupID.Value,
		"target_cluster_id": plan.TargetClusterID.Value,
	})

	restoreResp, response, err := apiClient.BackupApi.RestoreBackup(ctx, accountId, projectId).RestoreSpec(*restoreSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to restore backup", getErrorMessage(response, err))
		return
	}

	restoreId := *restoreResp.Data.Info.Id
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(120*time.Minute, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		restoreState, readOK, msg := getRestoreStateByID(ctx, accountId, projectId, restoreId, apiClient)
		if !readOK {
			return retry.RetryableError(errors.New("Unable to get restore state: " + msg))
		}
		if restoreState == "SUCCEEDED" {
			return nil
		}
		if restoreState == "FAILED" {
			return errors.New("restore failed")
		}
		return retry.RetryableError(errors.New("backup restore is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to restore backup", "The operation timed out or the restore failed.")
		return
	}

	plan.AccountID = types.String{Value: accountId}
	plan.ProjectID = types.String{Value: projectId}
	plan.RestoreID = types.String{Value: restoreId}

	diags := resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

func getRestoreStateByID(ctx context.Context, accountId, projectId, restoreId string, apiClient *openapiclient.APIClient) (state string, readOK bool, errorMessage string) {
	restoreResp, resp, err := apiClient.BackupApi.GetRestore(ctx, accountId, projectId, restoreId).Execute()
	if err != nil {
		return "", false, getErrorMessage(resp, err)
	}
	return string(restoreResp.Data.Info.GetState()), true, ""
}

func getIDsFromBackupRestoreState(ctx context.Context, state tfsdk.State, br *BackupRestore) {
	state.GetAttribute(ctx, path.Root("account_id"), &br.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &br.ProjectID)
	state.GetAttribute(ctx, path.Root("restore_id"), &br.RestoreID)
}

func (r resourceBackupRestore) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state BackupRestore
	getIDsFromBackupRestoreState(ctx, req.State, &state)
	req.State.GetAttribute(ctx, path.Root("backup_id"), &state.BackupID)
	req.State.GetAttribute(ctx, path.Root("target_cluster_id"), &state.TargetClusterID)
	req.State.GetAttribute(ctx, path.Root("use_roles"), &state.UseRoles)
	req.State.GetAttribute(ctx, path.Root("ysql_databases"), &state.YSQLDatabases)
	req.State.GetAttribute(ctx, path.Root("ycql_keyspaces"), &state.YCQLKeyspaces)
	req.State.GetAttribute(ctx, path.Root("ysql_databases_rename"), &state.YSQLDatabasesRename)
	req.State.GetAttribute(ctx, path.Root("ycql_keyspaces_rename"), &state.YCQLKeyspacesRename)

	restoreState, readOK, _ := getRestoreStateByID(ctx, state.AccountID.Value, state.ProjectID.Value, state.RestoreID.Value, r.p.client)
	if !readOK {
		// Restore may have been deleted or not found; remove from state
		resp.State.RemoveResource(ctx)
		return
	}
	if restoreState == "FAILED" {
		resp.Diagnostics.AddWarning("Restore failed", "The restore operation failed. The resource will be removed from state.")
		resp.State.RemoveResource(ctx)
		return
	}

	state.AccountID = types.String{Value: state.AccountID.Value}
	state.ProjectID = types.String{Value: state.ProjectID.Value}
	state.RestoreID = types.String{Value: state.RestoreID.Value}
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r resourceBackupRestore) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	resp.Diagnostics.AddError(
		"Update not supported",
		"Updating ybm_backup_restore is not supported. Change any attribute to trigger replace (destroy + create).",
	)
}

func (r resourceBackupRestore) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	// Delete only removes the resource from Terraform state; it does not call an API to "undo" the restore.
	resp.State.RemoveResource(ctx)
}

// listOfNonEmptyStringValidators rejects empty string elements in list(string) attributes.
func listOfNonEmptyStringValidators() []tfsdk.AttributeValidator {
	return []tfsdk.AttributeValidator{
		listvalidator.ValuesAre(stringvalidator.LengthAtLeast(1)),
	}
}

// nonEmptyStringValidators rejects "" for string attributes (e.g. rename names).
func nonEmptyStringValidators() []tfsdk.AttributeValidator {
	return []tfsdk.AttributeValidator{
		stringvalidator.LengthAtLeast(1),
	}
}
