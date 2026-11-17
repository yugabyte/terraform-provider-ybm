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
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceBackupRestore) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `Restore a backup to a cluster. All attributes are immutable; changing any attribute triggers destroy and create (a new restore). Use ysql_databases and ycql_keyspaces for selective restore; omit to restore all databases/keyspaces.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project.",
				Computed:    true,
			},
			"restore_id": schema.StringAttribute{
				Description: "The restore operation ID.",
				Computed:    true,
			},
			"backup_id": schema.StringAttribute{
				Description: "The ID of the backup to restore.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target_cluster_id": schema.StringAttribute{
				Description: "The ID of the cluster to restore the backup onto.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"use_roles": schema.BoolAttribute{
				Description: "Restore global YSQL roles. Defaults to false.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"ysql_databases": schema.ListAttribute{
				Description: "List of YSQL databases to restore. If empty or omitted, all YSQL databases are restored.",
				ElementType: types.StringType,
				Optional:    true,
				Validators:  listOfNonEmptyStringValidators(),
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"ycql_keyspaces": schema.ListAttribute{
				Description: "List of YCQL keyspaces to restore. If empty or omitted, all YCQL keyspaces are restored.",
				ElementType: types.StringType,
				Optional:    true,
				Validators:  listOfNonEmptyStringValidators(),
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"ysql_databases_rename": schema.ListNestedAttribute{
				Description: "List of YSQL database renames (backup_database -> restore_database).",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"backup_database": schema.StringAttribute{
							Description: "YSQL database name in the backup.",
							Required:    true,
							Validators:  nonEmptyStringValidators(),
						},
						"restore_database": schema.StringAttribute{
							Description: "YSQL database name to use on the restored cluster.",
							Required:    true,
							Validators:  nonEmptyStringValidators(),
						},
					},
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"ycql_keyspaces_rename": schema.ListNestedAttribute{
				Description: "List of YCQL keyspace renames (backup_database -> restore_database).",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"backup_database": schema.StringAttribute{
							Description: "YCQL keyspace name in the backup.",
							Required:    true,
							Validators:  nonEmptyStringValidators(),
						},
						"restore_database": schema.StringAttribute{
							Description: "YCQL keyspace name to use on the restored cluster.",
							Required:    true,
							Validators:  nonEmptyStringValidators(),
						},
					},
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func newResourceBackupRestore() resource.Resource {
	return &resourceBackupRestore{}
}

func (r *resourceBackupRestore) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backup_restore"
}

func (r *resourceBackupRestore) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
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
	restoreSpec.SetBackupId(plan.BackupID.ValueString())
	restoreSpec.SetClusterId(plan.TargetClusterID.ValueString())
	restoreSpec.SetUseRoles(plan.UseRoles.ValueBool())

	hasYSQL := len(plan.YSQLDatabases) > 0
	hasYCQL := len(plan.YCQLKeyspaces) > 0
	if hasYSQL || hasYCQL {
		sel := openapiclient.NewSelectiveRestoreSpec()
		if hasYSQL {
			ysql := make([]string, 0, len(plan.YSQLDatabases))
			for _, s := range plan.YSQLDatabases {
				if !s.IsNull() && !s.IsUnknown() {
					ysql = append(ysql, s.ValueString())
				}
			}
			sel.SetYsqlKeyspaces(ysql)
		}
		if hasYCQL {
			ycql := make([]string, 0, len(plan.YCQLKeyspaces))
			for _, s := range plan.YCQLKeyspaces {
				if !s.IsNull() && !s.IsUnknown() {
					ycql = append(ycql, s.ValueString())
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
				ysqlList = append(ysqlList, *openapiclient.NewKeyspaceRenameSpec(b.BackupDatabase.ValueString(), b.RestoreDatabase.ValueString()))
			}
			ksUpdate.SetYsql(ysqlList)
		}
		if hasYCQLRename {
			ycqlList := make([]openapiclient.KeyspaceRenameSpec, 0, len(plan.YCQLKeyspacesRename))
			for _, b := range plan.YCQLKeyspacesRename {
				ycqlList = append(ycqlList, *openapiclient.NewKeyspaceRenameSpec(b.BackupDatabase.ValueString(), b.RestoreDatabase.ValueString()))
			}
			ksUpdate.SetYcql(ycqlList)
		}
		restoreSpec.SetKeyspaceUpdateSpec(*ksUpdate)
	}

	return restoreSpec
}

func (r resourceBackupRestore) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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
	if plan.UseRoles.IsNull() || plan.UseRoles.IsUnknown() {
		plan.UseRoles = types.BoolValue(false)
	}

	restoreSpec := buildRestoreSpec(&plan)
	tflog.Debug(ctx, "Restoring backup to cluster", map[string]interface{}{
		"backup_id":         plan.BackupID.ValueString(),
		"target_cluster_id": plan.TargetClusterID.ValueString(),
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

	plan.AccountID = types.StringValue(accountId)
	plan.ProjectID = types.StringValue(projectId)
	plan.RestoreID = types.StringValue(restoreId)

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

func (r resourceBackupRestore) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state BackupRestore
	getIDsFromBackupRestoreState(ctx, req.State, &state)
	req.State.GetAttribute(ctx, path.Root("backup_id"), &state.BackupID)
	req.State.GetAttribute(ctx, path.Root("target_cluster_id"), &state.TargetClusterID)
	req.State.GetAttribute(ctx, path.Root("use_roles"), &state.UseRoles)
	req.State.GetAttribute(ctx, path.Root("ysql_databases"), &state.YSQLDatabases)
	req.State.GetAttribute(ctx, path.Root("ycql_keyspaces"), &state.YCQLKeyspaces)
	req.State.GetAttribute(ctx, path.Root("ysql_databases_rename"), &state.YSQLDatabasesRename)
	req.State.GetAttribute(ctx, path.Root("ycql_keyspaces_rename"), &state.YCQLKeyspacesRename)

	restoreState, readOK, _ := getRestoreStateByID(ctx, state.AccountID.ValueString(), state.ProjectID.ValueString(), state.RestoreID.ValueString(), r.p.client)
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

	state.AccountID = types.StringValue(state.AccountID.ValueString())
	state.ProjectID = types.StringValue(state.ProjectID.ValueString())
	state.RestoreID = types.StringValue(state.RestoreID.ValueString())
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r resourceBackupRestore) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update not supported",
		"Updating ybm_backup_restore is not supported. Change any attribute to trigger replace (destroy + create).",
	)
}

func (r resourceBackupRestore) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Delete only removes the resource from Terraform state; it does not call an API to "undo" the restore.
	resp.State.RemoveResource(ctx)
}

// listOfNonEmptyStringValidators rejects empty string elements in list(string) attributes.
func listOfNonEmptyStringValidators() []validator.List {
	return []validator.List{
		listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
	}
}

// nonEmptyStringValidators rejects "" for string attributes (e.g. rename names).
func nonEmptyStringValidators() []validator.String {
	return []validator.String{
		stringvalidator.LengthAtLeast(1),
	}
}
