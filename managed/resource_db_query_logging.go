package managed

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

type resourceDbQueryLoggingType struct{}

func (r resourceDbQueryLoggingType) GetSchema(ctx context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to manage DB query logging configuration for a cluster in YugabyteDB Aeon.`,
		Attributes: map[string]tfsdk.Attribute{
			"integration_name": {
				Description: "Name of the integration for this DB query logging configuration.",
				Type:        types.StringType,
				Required:    true,
			},
			"account_id": {
				Description: "ID of the account this DB query logging configuration belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "ID of the project this DB query logging configuration belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_id": {
				Description: "ID of the cluster with which this DB query logging config will be associated.",
				Type:        types.StringType,
				Required:    true,
			},
			"config_id": {
				Description: "ID of the DB query logging configuration. Created automatically when enabling DB query logs.",
				Type:        types.StringType,
				Computed:    true,
			},
			"state": {
				Description: "The status of the association of the cluster with DB query logging config.",
				Type:        types.StringType,
				Computed:    true,
			},
			"log_config": {
				Description: "The Log config.",
				Required:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"log_min_duration_statement": {
						Description: "Duration(in ms) of each completed statement to be logged if the statement ran for at least the specified amount of time.",
						Type:        types.Int64Type,
						Optional:    true,
						Computed:    true,
					},
					"debug_print_plan": {
						Description: "Enable debug print plan for statements.",
						Type:        types.BoolType,
						Optional:    true,
						Computed:    true,
					},
					"log_connections": {
						Description: "Log connections to the database.",
						Type:        types.BoolType,
						Optional:    true,
						Computed:    true,
					},
					"log_disconnections": {
						Description: "Log disconnections from the database.",
						Type:        types.BoolType,
						Optional:    true,
						Computed:    true,
					},
					"log_duration": {
						Description: "Log the duration of each statement.",
						Type:        types.BoolType,
						Optional:    true,
						Computed:    true,
					},
					"log_error_verbosity": {
						Description: "Sets the verbosity level of logged errors.",
						Type:        types.StringType,
						Optional:    true,
						Computed:    true,
						Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("DEFAULT", "TERSE", "VERBOSE")},
					},
					"log_statement": {
						Description: "The type of statements to be logged.",
						Type:        types.StringType,
						Optional:    true,
						Computed:    true,
						Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("NONE", "DDL", "MOD", "ALL")},
					},
					"log_min_error_statement": {
						Description: "Minimum severity for error statements to be logged.",
						Type:        types.StringType,
						Optional:    true,
						Computed:    true,
						Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("ERROR")},
					},
					"log_line_prefix": {
						Description: "A printf-style format string for log line prefixes.",
						Type:        types.StringType,
						Optional:    true,
						Computed:    true,
					},
				}),
			},
		},
	}, nil
}

func (r resourceDbQueryLoggingType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceDbQueryLogging{
		p: *(p.(*provider)),
	}, nil
}

type resourceDbQueryLogging struct {
	p provider
}

func getConfigFromPlan(ctx context.Context, plan tfsdk.Plan, config *DbQueryLoggingConfig) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &config.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("integration_name"), &config.IntegrationName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("log_config"), &config.LogConfig)...)
	return diags
}

func getConfigFromState(ctx context.Context, state tfsdk.State, config *DbQueryLoggingConfig) {
	state.GetAttribute(ctx, path.Root("account_id"), &config.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &config.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &config.ClusterID)
	state.GetAttribute(ctx, path.Root("integration_name"), &config.IntegrationName)
	state.GetAttribute(ctx, path.Root("config_id"), &config.ConfigID)
	state.GetAttribute(ctx, path.Root("log_config"), &config.LogConfig)
}

func buildDbQueryLoggingSpec(config DbQueryLoggingConfig, integrationId string, exportConfig *openapiclient.PgLogExportConfig) (*openapiclient.PgLogExporterConfigSpec, error) {
	newLogConfig := *exportConfig

	// Update log config params, that are provided in tf file
	if config.LogConfig != nil {
		tfLogConfig := *config.LogConfig
		if !tfLogConfig.LogConnections.IsNull() && !tfLogConfig.LogConnections.IsUnknown() {
			newLogConfig.SetLogConnections(tfLogConfig.LogConnections.Value)
		}

		if !tfLogConfig.DebugPrintPlan.IsNull() && !tfLogConfig.DebugPrintPlan.IsUnknown() {
			newLogConfig.SetDebugPrintPlan(tfLogConfig.DebugPrintPlan.Value)
		}

		if !tfLogConfig.LogDisconnections.IsNull() && !tfLogConfig.LogDisconnections.IsUnknown() {
			newLogConfig.SetLogDisconnections(tfLogConfig.LogDisconnections.Value)
		}

		if !tfLogConfig.LogDuration.IsNull() && !tfLogConfig.LogDuration.IsUnknown() {
			newLogConfig.SetLogDuration(tfLogConfig.LogDuration.Value)
		}

		if !tfLogConfig.LogMinDurationStatement.IsNull() && !tfLogConfig.LogMinDurationStatement.IsUnknown() {
			newLogConfig.SetLogMinDurationStatement(int32(tfLogConfig.LogMinDurationStatement.Value))
		}

		if !tfLogConfig.LogErrorVerbosity.IsNull() && !tfLogConfig.LogErrorVerbosity.IsUnknown() {
			logErrorVerbosityEnum, err := openapiclient.NewLogErrorVerbosityEnumFromValue(strings.ToUpper(tfLogConfig.LogErrorVerbosity.Value))
			if err != nil {
				return nil, err
			}
			newLogConfig.SetLogErrorVerbosity(*logErrorVerbosityEnum)
		}

		if !tfLogConfig.LogStatement.IsNull() && !tfLogConfig.LogStatement.IsUnknown() {
			logStatementEnum, err := openapiclient.NewLogStatementEnumFromValue(strings.ToUpper(tfLogConfig.LogStatement.Value))
			if err != nil {
				return nil, err
			}
			newLogConfig.SetLogStatement(*logStatementEnum)
		}

		if !tfLogConfig.LogMinErrorStatement.IsNull() && !tfLogConfig.LogMinErrorStatement.IsUnknown() {
			logMinErrorStatementEnum, err := openapiclient.NewLogMinErrorStatementEnumFromValue(strings.ToUpper(tfLogConfig.LogMinErrorStatement.Value))
			if err != nil {
				return nil, err
			}
			newLogConfig.SetLogMinErrorStatement(*logMinErrorStatementEnum)
		}

		if !tfLogConfig.LogLinePrefix.IsNull() && !tfLogConfig.LogLinePrefix.IsUnknown() {
			newLogConfig.SetLogLinePrefix(tfLogConfig.LogLinePrefix.Value)
		}
	}

	// Return the new PgLogExporterConfigSpec
	return &openapiclient.PgLogExporterConfigSpec{
		ExportConfig: newLogConfig,
		ExporterId:   integrationId,
	}, nil
}

func getPgLogExporterConfig(ctx context.Context, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (conf *openapiclient.PgLogExporterConfigData, ok bool, errMsg string) {
	specList, _, err := apiClient.ClusterApi.ListPgLogExporterConfigs(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return nil, false, GetApiErrorDetails(err)
	}

	if len(specList.GetData()) < 1 {
		return nil, false, "no DB query logging config found for the cluster"
	}
	return &specList.Data[0], true, ""
}

// Read latest state/config of a resource from Backend and convert it to model
func resourceRead(ctx context.Context, accountId string, projectId string, clusterId string,
	integrationName string,
	apiClient *openapiclient.APIClient) (dbQueryLoggingConfig DbQueryLoggingConfig, readOK bool, errMsg string) {

	spec, ok, errMsg := getPgLogExporterConfig(ctx, accountId, projectId, clusterId, apiClient)
	if !ok {
		return dbQueryLoggingConfig, false, errMsg
	}
	exportConfig := spec.Spec.ExportConfig

	// Initialize the LogConfig object from PgLogExportConfig
	logConfig := LogConfig{
		LogMinDurationStatement: types.Int64{Value: int64(exportConfig.LogMinDurationStatement)},
		DebugPrintPlan:          types.Bool{Value: exportConfig.DebugPrintPlan},
		LogConnections:          types.Bool{Value: exportConfig.LogConnections},
		LogDisconnections:       types.Bool{Value: exportConfig.LogDisconnections},
		LogDuration:             types.Bool{Value: exportConfig.LogDuration},
		LogErrorVerbosity:       types.String{Value: string(exportConfig.LogErrorVerbosity)},
		LogStatement:            types.String{Value: string(exportConfig.LogStatement)},
		LogMinErrorStatement:    types.String{Value: string(exportConfig.LogMinErrorStatement)},
		LogLinePrefix:           types.String{Value: exportConfig.LogLinePrefix},
	}

	dbQueryLoggingConfig = DbQueryLoggingConfig{
		AccountID:       types.String{Value: accountId},
		ProjectID:       types.String{Value: projectId},
		ClusterID:       types.String{Value: clusterId},
		IntegrationName: types.String{Value: integrationName},
		State:           types.String{Value: string(spec.Info.State)},
		ConfigID:        types.String{Value: spec.Info.Id},
		LogConfig:       &logConfig,
	}

	return dbQueryLoggingConfig, true, ""
}

func (r resourceDbQueryLogging) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state DbQueryLoggingConfig
	getConfigFromState(ctx, req.State, &state)
	apiClient := r.p.client
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	integrationName := state.IntegrationName.Value

	dbqlConfig, readOK, message := resourceRead(ctx, accountId, projectId, clusterId, integrationName, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Query log configuration associated with the cluster", message)
		return
	}

	diags := resp.State.Set(ctx, &dbqlConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceDbQueryLogging) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var planConfig DbQueryLoggingConfig
	resp.Diagnostics.Append(getConfigFromPlan(ctx, req.Plan, &planConfig)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the Db query logging config")
		return
	}
	integrationName := planConfig.IntegrationName.Value

	apiClient := r.p.client
	var stateConfig DbQueryLoggingConfig
	getConfigFromState(ctx, req.State, &stateConfig)
	accountId := stateConfig.AccountID.Value
	projectId := stateConfig.ProjectID.Value
	clusterId := stateConfig.ClusterID.Value
	configId := stateConfig.ConfigID.Value

	if planConfig.ClusterID != stateConfig.ClusterID {
		errMsg := "Field cluster_id cannot be changed after resource creation"
		resp.Diagnostics.AddError(errMsg, errMsg)
		return
	}

	spec, ok, errMsg := getPgLogExporterConfig(ctx, accountId, projectId, clusterId, apiClient)
	if !ok {
		resp.Diagnostics.AddError("Unable to fetch DB query logging config", errMsg)
		return
	}

	integrationId := ""
	if planConfig.IntegrationName != stateConfig.IntegrationName {
		integrationConfig, err := GetIntegrationDataByName(ctx, apiClient, accountId, projectId, integrationName)
		if err != nil {
			resp.Diagnostics.AddError("Unable to fetch integration details for: "+integrationName, err.Error())
			return
		}
		integrationId = integrationConfig.Info.Id
	} else {
		integrationId = spec.Spec.ExporterId
	}
	// Use planConfig provided in tf file to build new API Pg log exporter config spec
	apiConfigSpec, err := buildDbQueryLoggingSpec(planConfig, integrationId, &spec.Spec.ExportConfig)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update DB query logging config", GetApiErrorDetails(err))
		return
	}

	_, _, err = apiClient.ClusterApi.UpdatePgLogExporterConfig(ctx, accountId, projectId, clusterId, configId).PgLogExporterConfigSpec(*apiConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to update DB query logging config for cluster %s", clusterId), GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_EDIT_DATABASE_QUERY_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return fmt.Errorf("failed to update DB query log config")
			}
		} else {
			return retry.RetryableError(errors.New("unable to check DB query log config update status: " + message))
		}
		return retry.RetryableError(errors.New("DB query log config is being updated"))
	})

	if err != nil {
		errorSummary := fmt.Sprintf("Unable to update DB query log config for cluster: %s", clusterId)
		resp.Diagnostics.AddError(errorSummary, "The operation timed out waiting for DB query log config update operation.")
		return
	}

	planConfig.ConfigID.Value = configId

	dbqlConfig, readOK, readErrMsg := resourceRead(ctx, accountId, projectId, clusterId, integrationName, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Query log config ", readErrMsg)
		return
	}

	diags := resp.State.Set(ctx, &dbqlConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceDbQueryLogging) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	apiClient := r.p.client
	var state DbQueryLoggingConfig
	getConfigFromState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	configId := state.ConfigID.Value

	_, err := apiClient.ClusterApi.RemovePgLogExporterConfig(ctx, accountId, projectId, clusterId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to remove DB query logging config for cluster: %s", clusterId), GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_DISABLE_DATABASE_QUERY_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return retry.RetryableError(errors.New("Unable to check status for DB query logging removal task: " + message))
		}
		return retry.RetryableError(errors.New("DB Query log configuration is being removed from the cluster"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to remove Db Logging config from the cluster ", "The operation timed out waiting for DB  Query Logging removal to complete.")
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r resourceDbQueryLogging) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	resp.Diagnostics.AddError("Import is not currently supported", "")

}

// Create a new Db Query Log Configuration for a Cluster
func (r resourceDbQueryLogging) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var config DbQueryLoggingConfig
	resp.Diagnostics.Append(getConfigFromPlan(ctx, req.Plan, &config)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the DB query log configuration")
		return
	}

	apiClient := r.p.client
	var accountId, projectId string
	accountId, ok, message := getAccountId(ctx, apiClient)
	if !ok {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, ok, message = getProjectId(ctx, apiClient, accountId)
	if !ok {
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}

	var clusterId = config.ClusterID.Value
	var integrationId string

	integrationName := config.IntegrationName.Value

	integrationConfig, err := GetIntegrationDataByName(ctx, apiClient, accountId, projectId, integrationName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch integration details for: "+integrationName, err.Error())
		return
	}
	integrationId = integrationConfig.Info.Id

	dbQueryLoggingConfigSpec, err := buildDbQueryLoggingSpec(config, integrationId, openapiclient.NewPgLogExportConfigWithDefaults())
	if err != nil {
		tflog.Warn(ctx, "Unable to build DB query logging config spec"+GetApiErrorDetails(err))
		resp.Diagnostics.AddError("Encountered error while enabling DB Query Logging", GetApiErrorDetails(err))
		return
	}

	_, _, err = apiClient.ClusterApi.
		AssociatePgLogExporterConfig(ctx, accountId, projectId, clusterId).
		PgLogExporterConfigSpec(*dbQueryLoggingConfigSpec).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Failed to enable DB query logging for the cluster", GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)

	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_ENABLE_DATABASE_QUERY_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return fmt.Errorf("failed to enable DB query logging for cluster, operation failed")
			}
		} else {
			return retry.RetryableError(errors.New("unable to check status of DB Query Log configuration cluster association: " + message))
		}
		return retry.RetryableError(errors.New("DB query logging is being enabled for the cluster"))
	})

	if err != nil {
		errorSummary := fmt.Sprintf("Unable to enable DB Query Logging for the cluster: %s", clusterId)
		resp.Diagnostics.AddError(errorSummary, "The operation timed out waiting for DB Query Logging cluster association.")
		return
	}

	dbQueryLoggingConfig, readOK, readErrMsg := resourceRead(ctx, accountId, projectId,
		clusterId, integrationName, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Query log config for the cluster ", readErrMsg)
		return
	}

	diags := resp.State.Set(ctx, &dbQueryLoggingConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
