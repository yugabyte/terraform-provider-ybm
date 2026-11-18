/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	//"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (r *dataSourceDbAuditLogging) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The data source to fetch DB Audit log configuration for a cluster given cluster ID in YugabyteDB Aeon.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "ID of the account this DB Audit log configuration belongs to.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "ID of the project this DB Audit log configuration belongs to.",
				Computed:    true,
			},
			"cluster_name": schema.StringAttribute{
				Description: "Name of the cluster from which DB Audit Logs will be exported",
				Computed:    true,
			},
			"cluster_id": schema.StringAttribute{
				Description: "ID of the cluster from which DB Audit Logs will be exported",
				Required:    true,
			},
			"integration_name": schema.StringAttribute{
				Description: "Name of the integration to which the DB Audit Logs will be exported",
				Computed:    true,
			},
			"integration_id": schema.StringAttribute{
				Description: "ID of the integration to which the DB Audit Logs will be exported",
				Computed:    true,
			},
			"config_id": schema.StringAttribute{
				Description: "ID of the DB Audit log configuration",
				Computed:    true,
			},
			"state": schema.StringAttribute{
				Description: "The status of DB Audit Logging on the cluster",
				Computed:    true,
			},
			"ysql_config": schema.SingleNestedAttribute{
				Description: "The specification for a DB Audit ysql export configuration",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"statement_classes": schema.SetAttribute{
						Description: "List of ysql statements",
						ElementType: types.StringType,
						Computed:    true,
					},
					"log_settings": schema.SingleNestedAttribute{
						Description: "Db Audit Ysql Log Settings",
						Computed:    true,
						Attributes: map[string]schema.Attribute{
							"log_catalog": schema.BoolAttribute{
								Description: "These system catalog tables record system (as opposed to user) activity, such as metadata lookups and from third-party tools performing lookups",
								Computed:    true,
							},
							"log_client": schema.BoolAttribute{
								Description: "Enable this option to echo log messages directly to clients such as ysqlsh and psql",
								Computed:    true,
							},
							"log_relation": schema.BoolAttribute{
								Description: "Create separate log entries for each relation (TABLE, VIEW, and so on) referenced in a SELECT or DML statement",
								Computed:    true,
							},
							"log_level": schema.StringAttribute{
								Description: "Sets the severity level of logs written to clients",
								Computed:    true,
								Validators:  []validator.String{stringvalidator.OneOf("NOTICE", "WARNING", "LOG")},
							},
							"log_statement_once": schema.BoolAttribute{
								Description: "Enable this setting to only include statement text and parameters for the first entry for a statement or sub-statement combination",
								Computed:    true,
							},
							"log_parameter": schema.BoolAttribute{
								Description: "Include the parameters that were passed with the statement in the logs",
								Computed:    true,
							},
						},
					},
				},
			},
		},
	}
}

func newDataSourceDbAuditLogging() datasource.DataSource {
	return &dataSourceDbAuditLogging{}
}

func (r *dataSourceDbAuditLogging) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_db_audit_logging"
}

func (r *dataSourceDbAuditLogging) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type dataSourceDbAuditLogging struct {
	p provider
}

func (r dataSourceDbAuditLogging) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

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

	clusterId := dbAuditLoggingConfig.ClusterID.ValueString()
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
