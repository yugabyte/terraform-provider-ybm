package managed

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceDbQueryLogging) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Use this resource to configure database query logging for a cluster, and specify the export configuration (created using the `ybm_integration` resource) to use for log export. When assigned to a cluster, cluster query logs are exported to the sink defined in the export configuration.",
		Attributes: map[string]schema.Attribute{
			"integration_name": schema.StringAttribute{
				Description: "Name of the integration for this DB query logging configuration.",
				Required:    true,
			},
			"account_id": schema.StringAttribute{
				Description: "ID of the account this DB query logging configuration belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Description: "ID of the project this DB query logging configuration belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"cluster_id": schema.StringAttribute{
				Description: "ID of the cluster with which this DB query logging configuration will be associated.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"config_id": schema.StringAttribute{
				Description: "ID of the DB query logging configuration. Created automatically when enabling DB query logs.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"state": schema.StringAttribute{
				Description: "The status of the association of the cluster with DB query logging config.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"log_config": schema.SingleNestedAttribute{
				Description: "The Log config.",
				Required:    true,
				Attributes: map[string]schema.Attribute{
					"log_min_duration_statement": schema.Int64Attribute{
						Description: "Duration(in ms) of each completed statement to be logged if the statement ran for at least the specified amount of time.",
						Optional:    true,
						Computed:    true,
					},
					"debug_print_plan": schema.BoolAttribute{
						Description: "Enable debug print plan for statements.",
						Optional:    true,
						Computed:    true,
					},
					"log_connections": schema.BoolAttribute{
						Description: "Log connections to the database.",
						Optional:    true,
						Computed:    true,
					},
					"log_disconnections": schema.BoolAttribute{
						Description: "Log disconnections from the database.",
						Optional:    true,
						Computed:    true,
					},
					"log_duration": schema.BoolAttribute{
						Description: "Log the duration of each statement.",
						Optional:    true,
						Computed:    true,
					},
					"log_error_verbosity": schema.StringAttribute{
						Description: "Sets the verbosity level of logged errors.",
						Optional:    true,
						Computed:    true,
						Validators:  []validator.String{stringvalidator.OneOf("DEFAULT", "TERSE", "VERBOSE")},
					},
					"log_statement": schema.StringAttribute{
						Description: "The type of statements to be logged.",
						Optional:    true,
						Computed:    true,
						Validators:  []validator.String{stringvalidator.OneOf("NONE", "DDL", "MOD", "ALL")},
					},
					"log_min_error_statement": schema.StringAttribute{
						Description: "Minimum severity for error statements to be logged.",
						Optional:    true,
						Computed:    true,
						Validators:  []validator.String{stringvalidator.OneOf("ERROR")},
					},
					"log_line_prefix": schema.StringAttribute{
						Description: "A printf-style format string for log line prefixes.",
						Optional:    true,
						Computed:    true,
					},
				},
			},
		},
	}
}

func newResourceDbQueryLogging() resource.Resource {
	return &resourceDbQueryLogging{}
}

func (r *resourceDbQueryLogging) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_db_query_logging"
}

func (r *resourceDbQueryLogging) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
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
			newLogConfig.SetLogConnections(tfLogConfig.LogConnections.ValueBool())
		}

		if !tfLogConfig.DebugPrintPlan.IsNull() && !tfLogConfig.DebugPrintPlan.IsUnknown() {
			newLogConfig.SetDebugPrintPlan(tfLogConfig.DebugPrintPlan.ValueBool())
		}

		if !tfLogConfig.LogDisconnections.IsNull() && !tfLogConfig.LogDisconnections.IsUnknown() {
			newLogConfig.SetLogDisconnections(tfLogConfig.LogDisconnections.ValueBool())
		}

		if !tfLogConfig.LogDuration.IsNull() && !tfLogConfig.LogDuration.IsUnknown() {
			newLogConfig.SetLogDuration(tfLogConfig.LogDuration.ValueBool())
		}

		if !tfLogConfig.LogMinDurationStatement.IsNull() && !tfLogConfig.LogMinDurationStatement.IsUnknown() {
			newLogConfig.SetLogMinDurationStatement(int32(tfLogConfig.LogMinDurationStatement.ValueInt64()))
		}

		if !tfLogConfig.LogErrorVerbosity.IsNull() && !tfLogConfig.LogErrorVerbosity.IsUnknown() {
			logErrorVerbosityEnum, err := openapiclient.NewLogErrorVerbosityEnumFromValue(strings.ToUpper(tfLogConfig.LogErrorVerbosity.ValueString()))
			if err != nil {
				return nil, err
			}
			newLogConfig.SetLogErrorVerbosity(*logErrorVerbosityEnum)
		}

		if !tfLogConfig.LogStatement.IsNull() && !tfLogConfig.LogStatement.IsUnknown() {
			logStatementEnum, err := openapiclient.NewLogStatementEnumFromValue(strings.ToUpper(tfLogConfig.LogStatement.ValueString()))
			if err != nil {
				return nil, err
			}
			newLogConfig.SetLogStatement(*logStatementEnum)
		}

		if !tfLogConfig.LogMinErrorStatement.IsNull() && !tfLogConfig.LogMinErrorStatement.IsUnknown() {
			logMinErrorStatementEnum, err := openapiclient.NewLogMinErrorStatementEnumFromValue(strings.ToUpper(tfLogConfig.LogMinErrorStatement.ValueString()))
			if err != nil {
				return nil, err
			}
			newLogConfig.SetLogMinErrorStatement(*logMinErrorStatementEnum)
		}

		if !tfLogConfig.LogLinePrefix.IsNull() && !tfLogConfig.LogLinePrefix.IsUnknown() {
			newLogConfig.SetLogLinePrefix(tfLogConfig.LogLinePrefix.ValueString())
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
		return nil, false, "no DB query logging configuration found for the cluster"
	}
	return &specList.Data[0], true, ""
}

// Read latest state/config of a resource from Backend and convert it to model
func resourceRead(ctx context.Context, clusterId string, apiClient *openapiclient.APIClient) (
	dbQueryLoggingConfig DbQueryLoggingConfig, readOK bool, errMsg string) {

	accountId, getAccountOK, message := getAccountId(context.Background(), apiClient)
	if !getAccountOK {
		return dbQueryLoggingConfig, false, fmt.Sprintf("unable to get account ID %s", message)
	}

	projectId, getProjectOK, message := getProjectId(context.Background(), apiClient, accountId)
	if !getProjectOK {
		return dbQueryLoggingConfig, false, fmt.Sprintf("unable to get project ID %s", message)
	}

	spec, ok, errMsg := getPgLogExporterConfig(ctx, accountId, projectId, clusterId, apiClient)
	if !ok {
		return dbQueryLoggingConfig, false, errMsg
	}
	exportConfig := spec.Spec.ExportConfig

	configData, err := GetTelemetryProviderById(accountId, projectId, spec.Spec.ExporterId, apiClient)
	if err != nil {
		return dbQueryLoggingConfig, false, GetApiErrorDetails(err)
	}

	integrationName := configData.Spec.Name

	// Initialize the LogConfig object from PgLogExportConfig
	logConfig := LogConfig{
		LogMinDurationStatement: types.Int64Value(int64(exportConfig.LogMinDurationStatement)),
		DebugPrintPlan:          types.BoolValue(exportConfig.DebugPrintPlan),
		LogConnections:          types.BoolValue(exportConfig.LogConnections),
		LogDisconnections:       types.BoolValue(exportConfig.LogDisconnections),
		LogDuration:             types.BoolValue(exportConfig.LogDuration),
		LogErrorVerbosity:       types.StringValue(string(exportConfig.LogErrorVerbosity)),
		LogStatement:            types.StringValue(string(exportConfig.LogStatement)),
		LogMinErrorStatement:    types.StringValue(string(exportConfig.LogMinErrorStatement)),
		LogLinePrefix:           types.StringValue(exportConfig.LogLinePrefix),
	}

	dbQueryLoggingConfig = DbQueryLoggingConfig{
		AccountID:       types.StringValue(accountId),
		ProjectID:       types.StringValue(projectId),
		ClusterID:       types.StringValue(clusterId),
		IntegrationName: types.StringValue(integrationName),
		State:           types.StringValue(string(spec.Info.State)),
		ConfigID:        types.StringValue(spec.Info.Id),
		LogConfig:       &logConfig,
	}

	return dbQueryLoggingConfig, true, ""
}

func (r resourceDbQueryLogging) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DbQueryLoggingConfig
	getConfigFromState(ctx, req.State, &state)
	apiClient := r.p.client
	clusterId := state.ClusterID.ValueString()

	dbqlConfig, readOK, message := resourceRead(ctx, clusterId, apiClient)
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

func (r resourceDbQueryLogging) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var planConfig DbQueryLoggingConfig
	resp.Diagnostics.Append(getConfigFromPlan(ctx, req.Plan, &planConfig)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the Db query logging configuration")
		return
	}
	integrationName := planConfig.IntegrationName.ValueString()

	apiClient := r.p.client
	var stateConfig DbQueryLoggingConfig
	getConfigFromState(ctx, req.State, &stateConfig)
	accountId := stateConfig.AccountID.ValueString()
	projectId := stateConfig.ProjectID.ValueString()
	clusterId := stateConfig.ClusterID.ValueString()
	configId := stateConfig.ConfigID.ValueString()

	if planConfig.ClusterID != stateConfig.ClusterID {
		errMsg := "Field cluster_id cannot be changed after resource creation"
		resp.Diagnostics.AddError(errMsg, errMsg)
		return
	}

	spec, ok, errMsg := getPgLogExporterConfig(ctx, accountId, projectId, clusterId, apiClient)
	if !ok {
		resp.Diagnostics.AddError("Unable to fetch DB query logging configuration", errMsg)
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
		resp.Diagnostics.AddError("Unable to update DB query logging configuration", GetApiErrorDetails(err))
		return
	}

	_, _, err = apiClient.ClusterApi.UpdatePgLogExporterConfig(ctx, accountId, projectId, clusterId, configId).PgLogExporterConfigSpec(*apiConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to update DB query logging configuration for cluster %s", clusterId), GetApiErrorDetails(err))
		return
	}

	retryPolicy := clusterRunningTaskRetryPolicy(ctx, accountId, projectId, clusterId, apiClient)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_EDIT_DATABASE_QUERY_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return fmt.Errorf("failed to update DB query log configuration")
			}
		} else {
			return retry.RetryableError(errors.New("unable to check DB query log configuration update status: " + message))
		}
		return retry.RetryableError(errors.New("DB query log configuration is being updated"))
	})

	if err != nil {
		errorSummary := fmt.Sprintf("Unable to update DB query log configuration for cluster: %s", clusterId)
		resp.Diagnostics.AddError(errorSummary, "The operation timed out waiting for DB query log configuration update operation.")
		return
	}

	planConfig.ConfigID = types.StringValue(configId)

	dbqlConfig, readOK, readErrMsg := resourceRead(ctx, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Query log configuration ", readErrMsg)
		return
	}

	diags := resp.State.Set(ctx, &dbqlConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceDbQueryLogging) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	apiClient := r.p.client
	var state DbQueryLoggingConfig
	getConfigFromState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	clusterId := state.ClusterID.ValueString()
	configId := state.ConfigID.ValueString()

	_, err := apiClient.ClusterApi.RemovePgLogExporterConfig(ctx, accountId, projectId, clusterId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to remove DB query logging configuration for cluster: %s", clusterId), GetApiErrorDetails(err))
		return
	}

	retryPolicy := clusterRunningTaskRetryPolicy(ctx, accountId, projectId, clusterId, apiClient)
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
		resp.Diagnostics.AddError("Unable to remove Db Logging configuration from the cluster ", "The operation timed out waiting for DB  Query Logging removal to complete.")
		return
	}

	resp.State.RemoveResource(ctx)
}

// Import
func (r resourceDbQueryLogging) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the cluster_id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("cluster_id"), req, resp)
}

// Create a new Db Query Log Configuration for a Cluster
func (r resourceDbQueryLogging) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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

	var clusterId = config.ClusterID.ValueString()
	var integrationId string

	integrationName := config.IntegrationName.ValueString()

	integrationConfig, err := GetIntegrationDataByName(ctx, apiClient, accountId, projectId, integrationName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch integration details for: "+integrationName, err.Error())
		return
	}
	integrationId = integrationConfig.Info.Id

	dbQueryLoggingConfigSpec, err := buildDbQueryLoggingSpec(config, integrationId, openapiclient.NewPgLogExportConfigWithDefaults())
	if err != nil {
		tflog.Warn(ctx, "Unable to build DB query logging configuration spec"+GetApiErrorDetails(err))
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

	retryPolicy := clusterRunningTaskRetryPolicy(ctx, accountId, projectId, clusterId, apiClient)

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

	dbQueryLoggingConfig, readOK, readErrMsg := resourceRead(ctx, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of Db Query log configuration for the cluster ", readErrMsg)
		return
	}

	diags := resp.State.Set(ctx, &dbQueryLoggingConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
