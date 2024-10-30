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

type resourceDbAuditLoggingType struct{}

func (r resourceDbAuditLoggingType) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to manage DB Audit log configuration for a cluster in YugabyteDB Aeon.`,
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
				Required:    true,
			},
			"cluster_id": {
				Description: "ID of the cluster from which DB Audit Logs will be exported",
				Type:        types.StringType,
				Computed:    true,
			},
			"integration_name": {
				Description: "Name of the integration to which the DB Audit Logs will be exported",
				Type:        types.StringType,
				Required:    true,
			},
			"integration_id": {
				Description: "ID of the integration to which the DB Audit Logs will be exported",
				Type:        types.StringType,
				Computed:    true,
			},
			"config_id": {
				Description: "ID of the DB Audit logging configuration",
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
								Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("NOTICE", "WARNING", "LOG")},
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

func (r resourceDbAuditLoggingType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceDbAuditLogging{
		p: *(p.(*provider)),
	}, nil
}

type resourceDbAuditLogging struct {
	p provider
}

func getClusterDbAuditLogConfigPlan(ctx context.Context, plan tfsdk.Plan, dbAuditLoggingConfig *DbAuditLoggingConfig) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("ysql_config"), &dbAuditLoggingConfig.YsqlConfig)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_name"), &dbAuditLoggingConfig.ClusterName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("integration_name"), &dbAuditLoggingConfig.IntegrationName)...)
	return diags
}

func GetDbAuditYsqlLogSettings(plan DbAuditLoggingConfig) (*openapiclient.DbAuditYsqlLogSettings, error) {
	dbAuditLogSettings := openapiclient.NewDbAuditYsqlLogSettings()

	if plan.YsqlConfig == nil || plan.YsqlConfig.LogSettings == nil {
		return dbAuditLogSettings, nil
	}

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

func getDbAuditLoggingConfigSpec(plan DbAuditLoggingConfig) (*openapiclient.DbAuditExporterConfigSpec, error) {
	statementClasses, err := convertToDbAuditYsqlStatmentClassesEnum(plan.YsqlConfig.StatementClasses)
	if err != nil {
		return nil, fmt.Errorf("Statement classes provided are not supported: %v", err)
	}

	dbAuditLogSettings, err := GetDbAuditYsqlLogSettings(plan)
	if err != nil {
		return nil, fmt.Errorf("Log settings provided are not supported: %v", err)
	}

	return openapiclient.NewDbAuditExporterConfigSpec(*openapiclient.NewDbAuditYsqlExportConfig(statementClasses, *dbAuditLogSettings), plan.IntegrationId.Value), nil
}

// Create a new Db Audit Log Configuration for a Cluster
func (r resourceDbAuditLogging) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan DbAuditLoggingConfig
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

	integrationData, err := GetIntegrationDataByName(ctx, apiClient, accountId, projectId, plan.IntegrationName.Value)
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch integration details", err.Error())
		return
	}
	plan.IntegrationId.Value = integrationData.GetInfo().Id

	errMsg := fmt.Sprintf("Failed to enable DB Audit Logging on cluster %s", plan.ClusterName)

	clusterName := plan.ClusterName.Value
	clusterData, err := GetClusterByNameorID(accountId, projectId, "", clusterName, apiClient)
	if err != nil {
		resp.Diagnostics.AddError(errMsg, GetApiErrorDetails(err))
		return
	}
	clusterId := clusterData.GetInfo().Id

	dbAuditLoggingConfigSpec, err := getDbAuditLoggingConfigSpec(plan)
	if err != nil {
		resp.Diagnostics.AddError(errMsg, err.Error())
		return
	}

	_, _, err = apiClient.ClusterApi.AssociateDbAuditExporterConfig(ctx, accountId, projectId, clusterId).DbAuditExporterConfigSpec(*dbAuditLoggingConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError(errMsg, GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_ENABLE_DATABASE_AUDIT_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return fmt.Errorf(errMsg)
			}
		} else {
			return retry.RetryableError(errors.New("Unable to check the status of DB Audit Logging configuration: " + message))
		}
		return retry.RetryableError(errors.New("Enabling DB Audit Logging on the cluster " + clusterName))
	})

	if err != nil {
		resp.Diagnostics.AddError(errMsg, "The operation timed out waiting for while enabling DB Audit Logging")
		return
	}

	// configId := response.Data.Info.Id
	// plan.ConfigID.Value = configId

	dae, readOK, readErrMsg := resourceDbAuditLoggingRead(ctx, accountId, projectId, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to read the state of Db Audit logging on cluster %s ", clusterName), readErrMsg)
		return
	}

	diags := resp.State.Set(ctx, &dae)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func resourceDbAuditLoggingRead(ctx context.Context, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (dbAuditLoggingConfig DbAuditLoggingConfig, readOK bool, errMsg string) {
	listDbAuditLoggingConfigResp, _, err := apiClient.ClusterApi.ListDbAuditExporterConfig(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return dbAuditLoggingConfig, false, GetApiErrorDetails(err)
	}

	if len(listDbAuditLoggingConfigResp.GetData()) < 1 {
		return dbAuditLoggingConfig, false, fmt.Sprintf("Unable to find DB Audit Logging configuration for cluster with ID %s", clusterId)
	}

	dbAuditLoggingConfig.AccountID.Value = accountId
	dbAuditLoggingConfig.ProjectID.Value = projectId

	// Each cluster will have only one DB Audit Log config. We do not have a GET API for this feature
	data := listDbAuditLoggingConfigResp.GetData()[0]

	info := data.GetInfo()
	spec := data.GetSpec()

	dbAuditLoggingConfig.ConfigID.Value = info.Id
	dbAuditLoggingConfig.ClusterID.Value = info.ClusterId
	dbAuditLoggingConfig.State.Value = string(info.State)
	dbAuditLoggingConfig.IntegrationId.Value = spec.ExporterId

	integrationData, err := GetIntegrationByID(accountId, projectId, spec.ExporterId, apiClient)
	if err != nil {
		return dbAuditLoggingConfig, false, fmt.Sprintf("Failed to read DB Audit Logging configuration for cluster with ID %s", clusterId)
	}
	dbAuditLoggingConfig.IntegrationName.Value = integrationData.GetInfo().Id

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

	dbAuditLoggingConfig.YsqlConfig = &ysqlConfig

	return dbAuditLoggingConfig, true, ""
}

func getIDsFromDbAuditLoggingState(ctx context.Context, state tfsdk.State, dbAuditLoggingConfig *DbAuditLoggingConfig) {
	state.GetAttribute(ctx, path.Root("account_id"), &dbAuditLoggingConfig.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &dbAuditLoggingConfig.ProjectID)
	state.GetAttribute(ctx, path.Root("exporter_id"), &dbAuditLoggingConfig.IntegrationId)
	state.GetAttribute(ctx, path.Root("cluster_id"), &dbAuditLoggingConfig.ClusterID)
	state.GetAttribute(ctx, path.Root("config_id"), &dbAuditLoggingConfig.ConfigID)
}

// Read Db Audit log configuration for a cluster
func (r resourceDbAuditLogging) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state DbAuditLoggingConfig

	getIDsFromDbAuditLoggingState(ctx, req.State, &state)
	apiClient := r.p.client
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value

	dbalConfig, readOK, message := resourceDbAuditLoggingRead(ctx, accountId, projectId, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to read the state of Db Audit logging on the cluster %s", state.ClusterName.Value), message)
		return
	}

	diags := resp.State.Set(ctx, &dbalConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update Db Audit log configuration for a cluster
func (r resourceDbAuditLogging) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan DbAuditLoggingConfig
	resp.Diagnostics.Append(getClusterDbAuditLogConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the Db audit logging configuration")
		return
	}

	apiClient := r.p.client
	var state DbAuditLoggingConfig
	getIDsFromDbAuditLoggingState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	configId := state.ConfigID.Value
	clusterName := state.ClusterName.Value

	errMsg := fmt.Sprintf("Failed to update the DB Audit Logging configuration on cluster %s", clusterName)

	dbAuditLoggingConfigSpec, err := getDbAuditLoggingConfigSpec(plan)
	if err != nil {
		resp.Diagnostics.AddError(errMsg, err.Error())
		return
	}

	_, _, err = apiClient.ClusterApi.UpdateDbAuditExporterConfig(ctx, accountId, projectId, clusterId, configId).DbAuditExporterConfigSpec(*dbAuditLoggingConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError(errMsg, GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_EDIT_DATABASE_AUDIT_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return fmt.Errorf(errMsg)
			}
		} else {
			return retry.RetryableError(errors.New("Unable to check DB Audit Logging configuration update status: " + message))
		}
		return retry.RetryableError(errors.New("Updating the DB Audit Logging configuration on the cluster: " + clusterName))
	})

	if err != nil {
		resp.Diagnostics.AddError(errMsg, "The operation timed out waiting for DB Audit Log configuration update operation.")
		return
	}

	// plan.ConfigID.Value = configId

	dae, readOK, readErrMsg := resourceDbAuditLoggingRead(ctx, accountId, projectId, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to read the state of Db Audit logging on cluster %s ", clusterName), readErrMsg)
		return
	}

	diags := resp.State.Set(ctx, &dae)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete Db Audit Export Config for a cluster
func (r resourceDbAuditLogging) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	apiClient := r.p.client
	var state DbAuditLoggingConfig
	getIDsFromDbAuditLoggingState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	configId := state.ConfigID.Value
	clusterName := state.ClusterName.Value

	errMsg := fmt.Sprintf("Failed to disable DB Audit Logging on cluster %s", clusterName)

	_, _, err := apiClient.ClusterApi.RemoveDbAuditLogExporterConfig(ctx, accountId, projectId, clusterId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError(errMsg, GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_DISABLE_DATABASE_AUDIT_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return retry.RetryableError(errors.New("Unable to check the status of DB audit logging: " + message))
		}
		return retry.RetryableError(errors.New("Disabling DB Audit Logging on the cluster " + clusterName))
	})

	if err != nil {
		resp.Diagnostics.AddError(errMsg, "The operation timed out waiting for DB Audit Log configuration removal to complete.")
		return
	}

	resp.State.RemoveResource(ctx)
}

// Import
func (r resourceDbAuditLogging) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	resp.Diagnostics.AddError("Import is not currently supported", "")
}

func GetIntegrationByID(accountId string, projectId string, integrationId string, apiClient *openapiclient.APIClient) (*openapiclient.TelemetryProviderData, error) {
	resp, _, err := apiClient.TelemetryProviderApi.ListTelemetryProviders(context.Background(), accountId, projectId).Execute()

	if err != nil {
		return nil, err
	}

	for _, tpData := range resp.Data {
		if tpData.GetInfo().Id == integrationId {
			return &tpData, nil
		}
	}

	return nil, fmt.Errorf("Could not find integration with id: %s", integrationId)
}
