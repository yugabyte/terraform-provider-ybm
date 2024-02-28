package managed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceAssociateDbAuditExportConfigClusterType struct{}

func (r resourceAssociateDbAuditExportConfigClusterType) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to manage DB Audit log configuration for a cluster in YugabyteDB Managed.`,
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
				Description: "ID of the cluster with which this DB Audit log config will be associated ",
				Type:        types.StringType,
				Required:    true,
			},
			"exporter_id": {
				Description: "ID of the exporter to which the DB Audit logs will be exported",
				Type:        types.StringType,
				Required:    true,
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
				Required:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"statement_classes": {
						Description: "List of ysql statements",
						Type:        types.SetType{ElemType: types.StringType},
						Required:    true,
					},
					"log_settings": {
						Description: "Db Audit Ysql Log Settings",
						Optional:    true,
						Computed:    true,
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"log_catalog": {
								Description: "These system catalog tables record system (as opposed to user) activity, such as metadata lookups and from third-party tools performing lookups",
								Type:        types.BoolType,
								Optional:    true,
								Computed:    true,
							},
							"log_client": {
								Description: "Enable this option to echo log messages directly to clients such as ysqlsh and psql",
								Type:        types.BoolType,
								Optional:    true,
								Computed:    true,
							},
							"log_relation": {
								Description: "Create separate log entries for each relation (TABLE, VIEW, and so on) referenced in a SELECT or DML statement",
								Type:        types.BoolType,
								Optional:    true,
								Computed:    true,
							},
							"log_level": {
								Description: "Sets the severity level of logs written to clients",
								Type:        types.StringType,
								Optional:    true,
								Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("DEBUG1", "DEBUG2", "DEBUG3", "DEBUG4", "DEBUG5", "INFO", "NOTICE", "WARNING", "LOG")},
							},
							"log_statement_once": {
								Description: "Enable this setting to only include statement text and parameters for the first entry for a statement or sub-statement combination",
								Type:        types.BoolType,
								Optional:    true,
								Computed:    true,
							},
							"log_parameter": {
								Description: "Include the parameters that were passed with the statement in the logs",
								Type:        types.BoolType,
								Optional:    true,
								Computed:    true,
							},
						}),
					},
				}),
			},
		},
	}, nil
}

func (r resourceAssociateDbAuditExportConfigClusterType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceAssociateDbAuditExportConfigCluster{
		p: *(p.(*provider)),
	}, nil
}

type resourceAssociateDbAuditExportConfigCluster struct {
	p provider
}

func getClusterDbAuditLogConfigPlan(ctx context.Context, plan tfsdk.Plan, dbAuditExporterConfig *DbAuditExporterConfig) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("ysql_config"), &dbAuditExporterConfig.YsqlConfig)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("exporter_id"), &dbAuditExporterConfig.ExporterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &dbAuditExporterConfig.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("config_id"), &dbAuditExporterConfig.ConfigID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("state"), &dbAuditExporterConfig.State)...)
	return diags
}

func GetDbAuditYsqlLogSettings(plan DbAuditExporterConfig) (*openapiclient.DbAuditYsqlLogSettings, error) {
	if plan.YsqlConfig == nil || plan.YsqlConfig.LogSettings == nil {
		return nil, nil
	}

	dbAuditLogSettings := openapiclient.NewDbAuditYsqlLogSettings()

	// Set values from plan.YsqlConfig.LogSettings
	if !plan.YsqlConfig.LogSettings.LogCatalog.IsNull() && !plan.YsqlConfig.LogSettings.LogCatalog.IsUnknown() {
		dbAuditLogSettings.SetLogCatalog(plan.YsqlConfig.LogSettings.LogCatalog.Value)
	}
	if !plan.YsqlConfig.LogSettings.LogClient.IsNull() && !plan.YsqlConfig.LogSettings.LogClient.IsUnknown() {
		dbAuditLogSettings.SetLogClient(plan.YsqlConfig.LogSettings.LogClient.Value)
	}
	if !plan.YsqlConfig.LogSettings.LogLevel.IsNull() && !plan.YsqlConfig.LogSettings.LogLevel.IsUnknown() {
		logLevel, _ := openapiclient.NewDbAuditLogLevelEnumFromValue(plan.YsqlConfig.LogSettings.LogLevel.Value)
		dbAuditLogSettings.SetLogLevel(*logLevel)
	}
	if !plan.YsqlConfig.LogSettings.LogParameter.IsNull() && !plan.YsqlConfig.LogSettings.LogParameter.IsUnknown() {
		dbAuditLogSettings.SetLogParameter(plan.YsqlConfig.LogSettings.LogParameter.Value)
	}
	if !plan.YsqlConfig.LogSettings.LogRelation.IsNull() && !plan.YsqlConfig.LogSettings.LogRelation.IsUnknown() {
		dbAuditLogSettings.SetLogRelation(plan.YsqlConfig.LogSettings.LogRelation.Value)
	}
	if !plan.YsqlConfig.LogSettings.LogStatementOnce.IsNull() && !plan.YsqlConfig.LogSettings.LogStatementOnce.IsUnknown() {
		dbAuditLogSettings.SetLogStatementOnce(plan.YsqlConfig.LogSettings.LogStatementOnce.Value)
	}

	return dbAuditLogSettings, nil
}

func convertToDbAuditYsqlStatmentClassesEnum(statementClasses []types.String) ([]openapiclient.DbAuditYsqlStatmentClassesEnum, error) {
	var result []openapiclient.DbAuditYsqlStatmentClassesEnum

	for _, statement := range statementClasses {
		statementClassEnum, err := openapiclient.NewDbAuditYsqlStatmentClassesEnumFromValue(statement.Value)
		if err != nil {
			return nil, err
		}
		result = append(result, *statementClassEnum)
	}

	return result, nil
}

func getDbAuditExporterConfigSpec(plan DbAuditExporterConfig) (*openapiclient.DbAuditExporterConfigSpec, error) {
	statementClasses, err := convertToDbAuditYsqlStatmentClassesEnum(plan.YsqlConfig.StatementClasses)
	if err != nil {
		return nil, fmt.Errorf("error in converting statement classes string to DbAuditYsqlStatmentClassesEnum for cluster %s: %s", plan.ClusterID.Value, err)
	}

	dbAuditLogSettings, err := GetDbAuditYsqlLogSettings(plan)
	if err != nil {
		return nil, fmt.Errorf("error in obtaining LogSettings for cluster %s: %s", plan.ClusterID.Value, err)
	}

	dbAuditYsqlExportConfig := openapiclient.NewDbAuditYsqlExportConfig(statementClasses)
	if dbAuditLogSettings != nil {
		dbAuditYsqlExportConfig.SetLogSettings(*dbAuditLogSettings)
	}

	dbAuditExporterConfigSpec := openapiclient.NewDbAuditExporterConfigSpec(*dbAuditYsqlExportConfig, plan.ExporterID.Value)

	return dbAuditExporterConfigSpec, nil
}

// Create a new Db Audit Log Configuration for a Cluster
func (r resourceAssociateDbAuditExportConfigCluster) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan DbAuditExporterConfig
	var accountId, message string
	var getAccountOK bool

	resp.Diagnostics.Append(getClusterDbAuditLogConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the DB audit log configuration")
		return
	}

	apiClient := r.p.client
	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}

	exporterId := plan.ExporterID.Value

	_, err := GetConfigByNameorID(accountId, projectId, exporterId, "", apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Unable to associate db audit log configuration to cluster", GetApiErrorDetails(err))
		return
	}

	clusterId := plan.ClusterID.Value
	_, err = GetClusterByNameorID(accountId, projectId, clusterId, "", apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Unable to associate db audit log configuration to cluster", GetApiErrorDetails(err))
		return
	}

	dbAuditExporterConfigSpec, err := getDbAuditExporterConfigSpec(plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to obtain DbAuditExporterConfigSpec", GetApiErrorDetails(err))
		return
	}

	response, _, err := apiClient.ClusterApi.AssociateDbAuditExporterConfig(ctx, accountId, projectId, clusterId).DbAuditExporterConfigSpec(*dbAuditExporterConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to associate db audit log configuration to cluster", GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_MODIFY_DB_AUDIT_EXPORT_CONFIG, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return fmt.Errorf("unable to associate cluster with db audit log configuration, operation failed")
			}
		} else {
			return retry.RetryableError(errors.New("unable to get check db audit log configuration cluster association: " + message))
		}
		return retry.RetryableError(errors.New("db audit log config is being associated to the cluster"))
	})

	if err != nil {
		errorSummary := fmt.Sprintf("Unable to associate db audit log config to cluster: %s", clusterId)
		resp.Diagnostics.AddError(errorSummary, "The operation timed out waiting for db audit log config cluster association.")
		return
	}

	configId := response.Data.Info.Id
	plan.ConfigID.Value = configId

	dae, readOK, readErrMsg := resourceAssociateDbAuditExporterConfigClusterRead(ctx, accountId, projectId, clusterId, configId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Audit log config cluster association ", readErrMsg)
		return
	}

	diags := resp.State.Set(ctx, &dae)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func resourceAssociateDbAuditExporterConfigClusterRead(ctx context.Context, accountId string, projectId string, clusterId string, configId string, apiClient *openapiclient.APIClient) (dbAuditExporterConfig DbAuditExporterConfig, readOK bool, errMsg string) {
	listDbAuditExporterConfigResp, _, err := apiClient.ClusterApi.ListDbAuditExporterConfig(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return dbAuditExporterConfig, false, GetApiErrorDetails(err)
	}

	dbAuditExporterConfig.AccountID.Value = accountId
	dbAuditExporterConfig.ProjectID.Value = projectId

	for _, data := range listDbAuditExporterConfigResp.GetData() {
		info := data.GetInfo()
		spec := data.GetSpec()
		if info.Id == configId {
			dbAuditExporterConfig.ConfigID.Value = info.Id
			dbAuditExporterConfig.ClusterID.Value = info.ClusterId
			dbAuditExporterConfig.State.Value = string(info.State)
			dbAuditExporterConfig.ExporterID.Value = spec.ExporterId

			var logSettings LogSettings
			logSettings.LogCatalog.Value = *spec.YsqlConfig.LogSettings.LogCatalog
			logSettings.LogClient.Value = *spec.YsqlConfig.LogSettings.LogClient
			logSettings.LogStatementOnce.Value = *spec.YsqlConfig.LogSettings.LogStatementOnce
			logSettings.LogRelation.Value = *spec.YsqlConfig.LogSettings.LogRelation
			logSettings.LogParameter.Value = *spec.YsqlConfig.LogSettings.LogParameter
			logSettings.LogLevel.Value = string(*spec.YsqlConfig.LogSettings.LogLevel)

			var statementClasses []types.String
			for _, statementClass := range spec.GetYsqlConfig().StatementClasses {
				statementClasses = append(statementClasses, types.String{Value: string(statementClass)})
			}

			var ysqlConfig YsqlConfig
			ysqlConfig.StatementClasses = statementClasses
			ysqlConfig.LogSettings = &logSettings

			dbAuditExporterConfig.YsqlConfig = &ysqlConfig

			return dbAuditExporterConfig, true, ""
		}
	}
	return dbAuditExporterConfig, false, fmt.Sprintf("unable to find db audit log cluster association with id %s for cluster %s", configId, clusterId)
}

func getIDsFromAssocDbAuditExporterConfigClusterState(ctx context.Context, state tfsdk.State, dbe *DbAuditExporterConfig) {
	state.GetAttribute(ctx, path.Root("account_id"), &dbe.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &dbe.ProjectID)
	state.GetAttribute(ctx, path.Root("exporter_id"), &dbe.ConfigID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &dbe.ClusterID)
	state.GetAttribute(ctx, path.Root("config_id"), &dbe.ConfigID)
}

// Read Db Audit log configuration for a cluster
func (r resourceAssociateDbAuditExportConfigCluster) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state DbAuditExporterConfig

	getIDsFromAssocDbAuditExporterConfigClusterState(ctx, req.State, &state)
	apiClient := r.p.client
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	configId := state.ConfigID.Value
	clusterId := state.ClusterID.Value

	dbe, readOK, message := resourceAssociateDbAuditExporterConfigClusterRead(ctx, accountId, projectId, clusterId, configId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Audit log configuration associated with the cluster", message)
		return
	}

	diags := resp.State.Set(ctx, &dbe)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update Db Audit log configuration for a cluster
func (r resourceAssociateDbAuditExportConfigCluster) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan DbAuditExporterConfig
	resp.Diagnostics.Append(getClusterDbAuditLogConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the Db audit exporter config")
		return
	}

	apiClient := r.p.client
	var state DbAuditExporterConfig
	getIDsFromAssocDbAuditExporterConfigClusterState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	configId := state.ConfigID.Value

	dbAuditExporterConfigSpec, err := getDbAuditExporterConfigSpec(plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to obtain DbAuditExporterConfigSpec", GetApiErrorDetails(err))
		return
	}

	_, _, err = apiClient.ClusterApi.UpdateDbAuditExporterConfig(ctx, accountId, projectId, clusterId, configId).DbAuditExporterConfigSpec(*dbAuditExporterConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to update DB Audit Log Configuration with id: %s for cluster: %s", configId, clusterId), GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_MODIFY_DB_AUDIT_EXPORT_CONFIG, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return fmt.Errorf("unable to update db audit log configuration, operation failed")
			}
		} else {
			return retry.RetryableError(errors.New("unable to get check db audit log configuration update status: " + message))
		}
		return retry.RetryableError(errors.New("db audit log config is being updated"))
	})

	if err != nil {
		errorSummary := fmt.Sprintf("Unable to update DB Audit log config with id: %s to cluster: %s", configId, clusterId)
		resp.Diagnostics.AddError(errorSummary, "The operation timed out waiting for db audit log config update operation.")
		return
	}

	plan.ConfigID.Value = configId

	dae, readOK, readErrMsg := resourceAssociateDbAuditExporterConfigClusterRead(ctx, accountId, projectId, clusterId, configId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Audit log config cluster association ", readErrMsg)
		return
	}

	diags := resp.State.Set(ctx, &dae)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete Db Audit Export Config for a cluster
func (r resourceAssociateDbAuditExportConfigCluster) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	apiClient := r.p.client
	var state DbAuditExporterConfig
	getIDsFromAssocDbAuditExporterConfigClusterState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	configId := state.ConfigID.Value

	_, _, err := apiClient.ClusterApi.RemoveDbAuditLogExporterConfig(ctx, accountId, projectId, clusterId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to remove DB audit log exporter config with id: %s for cluster: %s", configId, clusterId), GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_MODIFY_DB_AUDIT_EXPORT_CONFIG, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to check DB audit log config removal: " + message))
		}
		return retry.RetryableError(errors.New("DB Audit log configuration is being removed from the cluster"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to de-associated metrics exporter from the cluster ", "The operation timed out waiting for metrics exporter cluster de-association.")
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import
func (r resourceAssociateDbAuditExportConfigCluster) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	resp.Diagnostics.AddError("Import is not currently supported", "")
}
