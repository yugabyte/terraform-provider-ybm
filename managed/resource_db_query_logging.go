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
			"cluster_name": {
				Description: "Name of the cluster with which this DB query logging config will be associated.",
				Type:        types.StringType,
				Required:    true,
			},
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
				Computed:    true,
			},
			"config_id": {
				Description: "ID of the DB query logging configuration.",
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
				Optional:    true,
				Computed:    true,
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
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_name"), &config.ClusterName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("integration_name"), &config.IntegrationName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("log_config"), &config.LogConfig)...)
	return diags
}

func buildDbQueryLoggingSpec(config DbQueryLoggingConfig, integrationId string) (*openapiclient.PgLogExporterConfigSpec, error) {
	// Start with a default log config
	newLogConfig := openapiclient.NewPgLogExportConfigWithDefaults()

	// Update log config params, that are provided in tf file
	if config.LogConfig != nil {
		tfLogConfig := *config.LogConfig
		if !tfLogConfig.LogConnections.IsNull() && !tfLogConfig.LogConnections.IsUnknown() {
			newLogConfig.SetLogConnections(tfLogConfig.LogConnections.Value)
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
		ExportConfig: *newLogConfig,
		ExporterId:   integrationId,
	}, nil
}

// Read latest state/config of a resource from Backend and convert it to model
func resourceRead(ctx context.Context, accountId string, projectId string, clusterId string,
	clusterName string, integrationName string,
	apiClient *openapiclient.APIClient) (dbAuditExporterConfig DbQueryLoggingConfig, readOK bool, errMsg string) {

	// fetch log config from backend
	specList, _, err := apiClient.ClusterApi.ListPgLogExporterConfigs(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return dbAuditExporterConfig, false, GetApiErrorDetails(err)
	}

	if len(specList.GetData()) == 0 {
		return dbAuditExporterConfig, false, fmt.Sprintf("No DB query logging config found for cluster %s", clusterName)
	}

	spec := specList.Data[0]
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

	dbAuditExporterConfig = DbQueryLoggingConfig{
		AccountID:       types.String{Value: accountId},
		ProjectID:       types.String{Value: projectId},
		ClusterID:       types.String{Value: clusterId},
		ClusterName:     types.String{Value: clusterName},
		IntegrationName: types.String{Value: integrationName},
		State:           types.String{Value: string(spec.Info.State)},
		ConfigID:        types.String{Value: spec.Info.Id},
		LogConfig:       &logConfig,
	}

	return dbAuditExporterConfig, true, ""
}

func getConfigFromState(ctx context.Context, state tfsdk.State, config *DbQueryLoggingConfig) {
	state.GetAttribute(ctx, path.Root("account_id"), &config.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &config.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_name"), &config.ClusterName)
	state.GetAttribute(ctx, path.Root("cluster_id"), &config.ClusterID)
	state.GetAttribute(ctx, path.Root("integration_name"), &config.IntegrationName)
	state.GetAttribute(ctx, path.Root("config_id"), &config.ConfigID)
}

func (r resourceDbQueryLogging) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state DbQueryLoggingConfig

	getConfigFromState(ctx, req.State, &state)
	apiClient := r.p.client
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	clusterName := state.ClusterName.Value
	integrationName := state.IntegrationName.Value

	dbe, readOK, message := resourceRead(ctx, accountId, projectId, clusterId, clusterName, integrationName, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Query log configuration associated with the cluster", message)
		return
	}

	diags := resp.State.Set(ctx, &dbe)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceDbQueryLogging) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	resp.Diagnostics.AddError(
		"Unsupported Operation",
		"Update is not currently supported.",
	)
}

func (r resourceDbQueryLogging) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	resp.Diagnostics.AddError(
		"Unsupported Operation",
		"Delete is not currently supported.",
	)
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

	var clusterId string
	var integrationId string

	clusterName := config.ClusterName.Value
	integrationName := config.IntegrationName.Value
	clusterData, err := GetClusterByNameorID(accountId, projectId, "", clusterName, apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Unable to get cluster id for cluster: "+clusterName, GetApiErrorDetails(err))
		return
	}
	clusterId = clusterData.Info.Id

	integrationConfig, _, err := apiClient.TelemetryProviderApi.
		ListTelemetryProviders(context.Background(), accountId, projectId).
		Name(integrationName).
		Execute()

	if err != nil {
		resp.Diagnostics.AddError("Unable to get integration ID for integration: "+integrationName, GetApiErrorDetails(err))
		return
	}

	if len(integrationConfig.GetData()) < 1 {
		message := fmt.Sprintf("Integration %s not found", integrationName)
		resp.Diagnostics.AddError(message, message)
		return
	}
	integrationId = integrationConfig.GetData()[0].Info.Id

	dbQueryLoggingConfigSpec, err := buildDbQueryLoggingSpec(config, integrationId)
	if err != nil {
		resp.Diagnostics.AddError("Unable to build DB query logging config spec", GetApiErrorDetails(err))
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
		clusterId, clusterName, integrationName, apiClient)
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
