/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	//"github.com/hashicorp/terraform-plugin-log/tflog"
)

type dataSourceDbAuditLoggingType struct{}

func (r dataSourceDbAuditLoggingType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The data source to fetch DB Audit log configuration for a cluster given cluster ID in YugabyteDB Aeon.`,
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
			"cluster_name": {
				Description: "Name of the cluster from which DB Audit Logs will be exported",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_id": {
				Description: "ID of the cluster from which DB Audit Logs will be exported",
				Type:        types.StringType,
				Required:    true,
			},
			"integration_name": {
				Description: "Name of the integration to which the DB Audit Logs will be exported",
				Type:        types.StringType,
				Computed:    true,
			},
			"integration_id": {
				Description: "ID of the integration to which the DB Audit Logs will be exported",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_id": {
				Description: "ID of the DB Audit log configuration",
				Type:        types.StringType,
				Computed:    true,
			},
			"state": {
				Description: "The status of DB Audit Logging on the cluster",
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

func (r dataSourceDbAuditLoggingType) NewDataSource(_ context.Context, p tfsdk.Provider) (tfsdk.DataSource, diag.Diagnostics) {
	return dataSourceDbAuditLogging{
		p: *(p.(*provider)),
	}, nil
}

type dataSourceDbAuditLogging struct {
	p provider
}

func (r dataSourceDbAuditLogging) Read(ctx context.Context, req tfsdk.ReadDataSourceRequest, resp *tfsdk.ReadDataSourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another data source.",
		)
		return
	}

	var dbAuditLoggingConfig DbAuditLoggingConfig

	diags := req.Config.Get(ctx, &dbAuditLoggingConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client

	clusterId := dbAuditLoggingConfig.ClusterID.Value
	dbAuditLoggingConfig, readOK, err := resourceDbAuditLoggingRead(ctx, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to read the state of Db Audit logging on cluster %s ", clusterId), err.Error())
		return
	}

	diags = resp.State.Set(ctx, &dbAuditLoggingConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
