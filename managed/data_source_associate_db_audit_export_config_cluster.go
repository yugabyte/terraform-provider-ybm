/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	//"github.com/hashicorp/terraform-plugin-log/tflog"
)

type dataSourceAssociateDbAuditExportConfigClusterType struct{}

func (r dataSourceAssociateDbAuditExportConfigClusterType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The data source to fetch DB Audit log configuration for a cluster given cluster ID or configuration ID in YugabyteDB Aeon.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "ID of the account this DB Audit log configuration belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "ID of the project this DB Audit log configuration belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_id": {
				Description: "ID of the cluster with which this DB Audit log config is associated ",
				Type:        types.StringType,
				Required:    true,
			},
			"exporter_id": {
				Description: "ID of the exporter to which the DB Audit logs will be exported",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_id": {
				Description: "ID of the DB Audit log configuration",
				Type:        types.StringType,
				Computed:    true,
			},
			"state": {
				Description: "The stutus of association of cluster with DB Audit log config",
				Type:        types.StringType,
				Computed:    true,
			},
			"ysql_config": {
				Description: "The specification for a DB Audit ysql export configuration",
				Computed:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"statement_classes": {
						Description: "List of ysql statements",
						Type:        types.SetType{ElemType: types.StringType},
						Computed:    true,
					},
					"log_settings": {
						Description: "Db Audit Ysql Log Settings",
						Computed:    true,
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"log_catalog": {
								Description: "These system catalog tables record system (as opposed to user) activity, such as metadata lookups and from third-party tools performing lookups",
								Type:        types.BoolType,
								Computed:    true,
							},
							"log_client": {
								Description: "Enable this option to echo log messages directly to clients such as ysqlsh and psql",
								Type:        types.BoolType,
								Computed:    true,
							},
							"log_relation": {
								Description: "Create separate log entries for each relation (TABLE, VIEW, and so on) referenced in a SELECT or DML statement",
								Type:        types.BoolType,
								Computed:    true,
							},
							"log_level": {
								Description: "Sets the severity level of logs written to clients",
								Type:        types.StringType,
								Computed:    true,
								Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("NOTICE", "WARNING", "LOG")},
							},
							"log_statement_once": {
								Description: "Enable this setting to only include statement text and parameters for the first entry for a statement or sub-statement combination",
								Type:        types.BoolType,
								Computed:    true,
							},
							"log_parameter": {
								Description: "Include the parameters that were passed with the statement in the logs",
								Type:        types.BoolType,
								Computed:    true,
							},
						}),
					},
				}),
			},
		},
	}, nil
}

func (r dataSourceAssociateDbAuditExportConfigClusterType) NewDataSource(_ context.Context, p tfsdk.Provider) (tfsdk.DataSource, diag.Diagnostics) {
	return dataSourceAssociateDbAuditExportConfigCluster{
		p: *(p.(*provider)),
	}, nil
}

type dataSourceAssociateDbAuditExportConfigCluster struct {
	p provider
}

func (r dataSourceAssociateDbAuditExportConfigCluster) Read(ctx context.Context, req tfsdk.ReadDataSourceRequest, resp *tfsdk.ReadDataSourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another data source.",
		)
		return
	}

	var daeConfig DbAuditExporterConfig

	diags := req.Config.Get(ctx, &daeConfig)
	resp.Diagnostics.Append(diags...)
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
		resp.Diagnostics.AddError("Unable to get the project ID ", message)
		return
	}

	clusterId := daeConfig.ClusterID.Value
	_, err := GetClusterByNameorID(accountId, projectId, clusterId, "", apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch DB Audit Log configuration data", GetApiErrorDetails(err))
		return
	}

	daeConfig, readOK, message := resourceAssociateDbAuditExporterConfigClusterRead(ctx, accountId, projectId, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the DB Audit Log Configuration associated to the cluster", message)
		return
	}

	diags = resp.State.Set(ctx, &daeConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
