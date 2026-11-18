package managed

import (
	"context"
	"errors"
	"fmt"

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

var validStatementClasses = []string{
	string(openapiclient.DBAUDITYSQLSTATMENTCLASSESENUM_READ),
	string(openapiclient.DBAUDITYSQLSTATMENTCLASSESENUM_WRITE),
	string(openapiclient.DBAUDITYSQLSTATMENTCLASSESENUM_ROLE),
	string(openapiclient.DBAUDITYSQLSTATMENTCLASSESENUM_FUNCTION),
	string(openapiclient.DBAUDITYSQLSTATMENTCLASSESENUM_DDL),
	string(openapiclient.DBAUDITYSQLSTATMENTCLASSESENUM_MISC),
}

func (r *resourceDbAuditLogging) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Use this resource to configure database audit logging for a cluster, and specify the export configuration (created using the `ybm_integration` resource) to use for log export. When assigned to a cluster, cluster audit logs are exported to the sink defined in the export configuration.",
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "ID of the account this DB Audit log configuration belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Description: "ID of the project this DB Audit log configuration belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"cluster_name": schema.StringAttribute{
				Description: "Name of the cluster from which DB Audit Logs will be exported",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"cluster_id": schema.StringAttribute{
				Description: "ID of the cluster from which DB Audit Logs will be exported",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"integration_name": schema.StringAttribute{
				Description: "Name of the integration to which the DB Audit Logs will be exported",
				Required:    true,
			},
			"integration_id": schema.StringAttribute{
				Description: "ID of the integration to which the DB Audit Logs will be exported",
				Computed:    true,
			},
			"config_id": schema.StringAttribute{
				Description: "ID of the DB Audit logging configuration",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"state": schema.StringAttribute{
				Description: "The status of DB Audit Logging on the cluster",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"ysql_config": schema.SingleNestedAttribute{
				Description: "The specification for a DB Audit ysql export configuration",
				Required:    true,
				Attributes: map[string]schema.Attribute{
					"statement_classes": schema.SetAttribute{
						Description: "List of ysql statements",
						ElementType: types.StringType,
						Required:    true,
					},
					"log_settings": schema.SingleNestedAttribute{
						Description: "Db Audit Ysql Log Settings",
						Required:    true,
						Attributes: map[string]schema.Attribute{
							"log_catalog": schema.BoolAttribute{
								Description: "These system catalog tables record system (as opposed to user) activity, such as metadata lookups and from third-party tools performing lookups. Defaults to 'true' if not specified.",
								Optional:    true,
								Computed:    true,
							},
							"log_client": schema.BoolAttribute{
								Description: "Enable this option to echo log messages directly to clients such as ysqlsh and psql. Defaults to 'true' if not specified",
								Optional:    true,
								Computed:    true,
							},
							"log_relation": schema.BoolAttribute{
								Description: "Create separate log entries for each relation (TABLE, VIEW, and so on) referenced in a SELECT or DML statement. Defaults to 'false' if not specified",
								Optional:    true,
								Computed:    true,
							},
							"log_level": schema.StringAttribute{
								Description: "Sets the severity level of logs written to clients. Defaults to 'LOG' if not specified",
								Optional:    true,
								Computed:    true,
								Validators:  []validator.String{stringvalidator.OneOf("NOTICE", "WARNING", "LOG")},
							},
							"log_statement_once": schema.BoolAttribute{
								Description: "Enable this setting to only include statement text and parameters for the first entry for a statement or sub-statement combination. Defaults to 'false' if not specified",
								Optional:    true,
								Computed:    true,
							},
							"log_parameter": schema.BoolAttribute{
								Description: "Include the parameters that were passed with the statement in the logs. Defaults to 'false' if not specified",
								Optional:    true,
								Computed:    true,
							},
						},
					},
				},
			},
		},
	}
}

func newResourceDbAuditLogging() resource.Resource {
	return &resourceDbAuditLogging{}
}

func (r *resourceDbAuditLogging) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_db_audit_logging"
}

func (r *resourceDbAuditLogging) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceDbAuditLogging struct {
	p provider
}

func getClusterDbAuditLogConfigPlan(ctx context.Context, plan tfsdk.Plan, dbAuditLoggingConfig *DbAuditLoggingConfig) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("ysql_config"), &dbAuditLoggingConfig.YsqlConfig)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &dbAuditLoggingConfig.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("integration_name"), &dbAuditLoggingConfig.IntegrationName)...)
	return diags
}

func GetDbAuditYsqlLogSettings(plan DbAuditLoggingConfig) (*openapiclient.DbAuditYsqlLogSettings, error) {
	dbAuditLogSettings := openapiclient.NewDbAuditYsqlLogSettings()

	if plan.YsqlConfig == nil || plan.YsqlConfig.LogSettings == nil {
		return dbAuditLogSettings, nil
	}

	if !plan.YsqlConfig.LogSettings.LogCatalog.IsNull() && !plan.YsqlConfig.LogSettings.LogCatalog.IsUnknown() {
		dbAuditLogSettings.SetLogCatalog(plan.YsqlConfig.LogSettings.LogCatalog.ValueBool())
	}
	if !plan.YsqlConfig.LogSettings.LogClient.IsNull() && !plan.YsqlConfig.LogSettings.LogClient.IsUnknown() {
		dbAuditLogSettings.SetLogClient(plan.YsqlConfig.LogSettings.LogClient.ValueBool())
	}
	if !plan.YsqlConfig.LogSettings.LogLevel.IsNull() && !plan.YsqlConfig.LogSettings.LogLevel.IsUnknown() {
		logLevel, _ := openapiclient.NewDbAuditLogLevelEnumFromValue(plan.YsqlConfig.LogSettings.LogLevel.ValueString())
		dbAuditLogSettings.SetLogLevel(*logLevel)
	}
	if !plan.YsqlConfig.LogSettings.LogParameter.IsNull() && !plan.YsqlConfig.LogSettings.LogParameter.IsUnknown() {
		dbAuditLogSettings.SetLogParameter(plan.YsqlConfig.LogSettings.LogParameter.ValueBool())
	}
	if !plan.YsqlConfig.LogSettings.LogRelation.IsNull() && !plan.YsqlConfig.LogSettings.LogRelation.IsUnknown() {
		dbAuditLogSettings.SetLogRelation(plan.YsqlConfig.LogSettings.LogRelation.ValueBool())
	}
	if !plan.YsqlConfig.LogSettings.LogStatementOnce.IsNull() && !plan.YsqlConfig.LogSettings.LogStatementOnce.IsUnknown() {
		dbAuditLogSettings.SetLogStatementOnce(plan.YsqlConfig.LogSettings.LogStatementOnce.ValueBool())
	}

	return dbAuditLogSettings, nil
}

func convertToDbAuditYsqlStatmentClassesEnum(statementClasses []types.String) ([]openapiclient.DbAuditYsqlStatmentClassesEnum, error) {
	var result []openapiclient.DbAuditYsqlStatmentClassesEnum
	if len(statementClasses) < 1 {
		return result, fmt.Errorf("At least one statement class must be provided")
	}

	for _, statement := range statementClasses {
		statementClassEnum, err := openapiclient.NewDbAuditYsqlStatmentClassesEnumFromValue(statement.ValueString())
		if err != nil {
			return nil, fmt.Errorf("Invalid statement class '%s'. Valid values are: %v", statement.ValueString(), validStatementClasses)
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

	return openapiclient.NewDbAuditExporterConfigSpec(*openapiclient.NewDbAuditYsqlExportConfig(statementClasses, *dbAuditLogSettings), plan.IntegrationId.ValueString()), nil
}

// Create a new Db Audit Log Configuration for a Cluster
func (r resourceDbAuditLogging) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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
		tflog.Error(ctx, "Error while getting the plan for the DB audit log configuration")
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

	clusterId := plan.ClusterID.ValueString()
	clusterData, err := GetClusterByNameorID(accountId, projectId, clusterId, "", apiClient)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to fetch details of cluster with id : %s", clusterId), GetApiErrorDetails(err))
		return
	}
	clusterName := clusterData.Spec.Name
	errMsg := fmt.Sprintf("Failed to enable DB Audit Logging on cluster %s", clusterName)

	integrationData, err := GetIntegrationDataByName(ctx, apiClient, accountId, projectId, plan.IntegrationName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch integration details", err.Error())
		return
	}
	plan.IntegrationId = types.StringValue(integrationData.GetInfo().Id)

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

	retryPolicy := clusterRunningTaskRetryPolicy(ctx, accountId, projectId, clusterId, apiClient)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_ENABLE_DATABASE_AUDIT_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return errors.New(errMsg)
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

	dae, readOK, err := resourceDbAuditLoggingRead(ctx, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to read the state of Db Audit logging on cluster %s ", clusterName), err.Error())
		return
	}

	diags := resp.State.Set(ctx, &dae)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func resourceDbAuditLoggingRead(ctx context.Context, clusterId string, apiClient *openapiclient.APIClient) (dbAuditLoggingConfig DbAuditLoggingConfig, readOK bool, error error) {

	accountId, getAccountOK, message := getAccountId(context.Background(), apiClient)
	if !getAccountOK {
		return dbAuditLoggingConfig, false, fmt.Errorf("unable to get account ID %s", message)
	}

	projectId, getProjectOK, message := getProjectId(context.Background(), apiClient, accountId)
	if !getProjectOK {
		return dbAuditLoggingConfig, false, fmt.Errorf("unable to get project ID %s", message)
	}

	clusterData, err := GetClusterByNameorID(accountId, projectId, clusterId, "", apiClient)
	if err != nil {
		return dbAuditLoggingConfig, false, errors.New(GetApiErrorDetails(err))
	}
	clusterName := clusterData.GetSpec().Name

	listDbAuditLoggingConfigResp, _, err := apiClient.ClusterApi.ListDbAuditExporterConfig(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return dbAuditLoggingConfig, false, errors.New(GetApiErrorDetails(err))
	}

	if len(listDbAuditLoggingConfigResp.GetData()) < 1 {
		return dbAuditLoggingConfig, false, fmt.Errorf("Unable to find DB Audit Logging configuration for cluster %s", clusterName)
	}

	dbAuditLoggingConfig.AccountID = types.StringValue(accountId)
	dbAuditLoggingConfig.ProjectID = types.StringValue(projectId)

	// Each cluster will have only one DB Audit Log config. We do not have a GET API for this feature
	data := listDbAuditLoggingConfigResp.GetData()[0]

	info := data.GetInfo()
	spec := data.GetSpec()

	dbAuditLoggingConfig.ConfigID = types.StringValue(info.Id)
	dbAuditLoggingConfig.ClusterID = types.StringValue(info.ClusterId)
	dbAuditLoggingConfig.State = types.StringValue(string(info.State))
	dbAuditLoggingConfig.IntegrationId = types.StringValue(spec.ExporterId)
	dbAuditLoggingConfig.ClusterName = types.StringValue(clusterName)

	integrationData, err := GetIntegrationByID(accountId, projectId, spec.ExporterId, apiClient)
	if err != nil {
		return dbAuditLoggingConfig, false, errors.New(GetApiErrorDetails(err))
	}
	dbAuditLoggingConfig.IntegrationName = types.StringValue(integrationData.GetSpec().Name)

	var logSettings LogSettings
	logSettings.LogCatalog = types.BoolValue(*spec.YsqlConfig.LogSettings.LogCatalog)
	logSettings.LogClient = types.BoolValue(*spec.YsqlConfig.LogSettings.LogClient)
	logSettings.LogStatementOnce = types.BoolValue(*spec.YsqlConfig.LogSettings.LogStatementOnce)
	logSettings.LogRelation = types.BoolValue(*spec.YsqlConfig.LogSettings.LogRelation)
	logSettings.LogParameter = types.BoolValue(*spec.YsqlConfig.LogSettings.LogParameter)
	logSettings.LogLevel = types.StringValue(string(*spec.YsqlConfig.LogSettings.LogLevel))

	var statementClasses []types.String
	for _, statementClass := range spec.GetYsqlConfig().StatementClasses {
		statementClasses = append(statementClasses, types.StringValue(string(statementClass)))
	}

	var ysqlConfig YsqlConfig
	ysqlConfig.StatementClasses = statementClasses
	ysqlConfig.LogSettings = &logSettings

	dbAuditLoggingConfig.YsqlConfig = &ysqlConfig

	return dbAuditLoggingConfig, true, nil
}

func getIDsFromDbAuditLoggingState(ctx context.Context, state tfsdk.State, dbAuditLoggingConfig *DbAuditLoggingConfig) {
	state.GetAttribute(ctx, path.Root("account_id"), &dbAuditLoggingConfig.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &dbAuditLoggingConfig.ProjectID)
	state.GetAttribute(ctx, path.Root("integration_id"), &dbAuditLoggingConfig.IntegrationId)
	state.GetAttribute(ctx, path.Root("cluster_id"), &dbAuditLoggingConfig.ClusterID)
	state.GetAttribute(ctx, path.Root("config_id"), &dbAuditLoggingConfig.ConfigID)
}

// Read Db Audit log configuration for a cluster
func (r resourceDbAuditLogging) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DbAuditLoggingConfig

	getIDsFromDbAuditLoggingState(ctx, req.State, &state)
	apiClient := r.p.client
	clusterId := state.ClusterID.ValueString()

	dbalConfig, readOK, err := resourceDbAuditLoggingRead(ctx, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to read the state of Db Audit logging on the cluster %s", state.ClusterName.ValueString()), err.Error())
		return
	}

	diags := resp.State.Set(ctx, &dbalConfig)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update Db Audit log configuration for a cluster
func (r resourceDbAuditLogging) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DbAuditLoggingConfig
	resp.Diagnostics.Append(getClusterDbAuditLogConfigPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Error(ctx, "Error while getting the plan for the Db audit logging configuration")
		return
	}

	apiClient := r.p.client
	var state DbAuditLoggingConfig
	getIDsFromDbAuditLoggingState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	clusterId := state.ClusterID.ValueString()
	configId := state.ConfigID.ValueString()
	clusterName := state.ClusterName.ValueString()

	if plan.ClusterID != state.ClusterID {
		resp.Diagnostics.AddError("'cluster_id' cannot be updated", "'cluster_id' cannot be updated when updating DB Audit Log configuration")
	}

	errMsg := fmt.Sprintf("Failed to update the DB Audit Logging configuration on cluster %s", clusterName)

	integrationData, err := GetIntegrationDataByName(ctx, apiClient, accountId, projectId, plan.IntegrationName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch integration details", err.Error())
		return
	}
	plan.IntegrationId = types.StringValue(integrationData.GetInfo().Id)

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

	retryPolicy := clusterRunningTaskRetryPolicy(ctx, accountId, projectId, clusterId, apiClient)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_EDIT_DATABASE_AUDIT_LOGGING, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return errors.New(errMsg)
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

	dae, readOK, err := resourceDbAuditLoggingRead(ctx, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to read the state of Db Audit logging on cluster %s ", clusterName), err.Error())
		return
	}

	diags := resp.State.Set(ctx, &dae)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete Db Audit Log Configuration for a cluster
func (r resourceDbAuditLogging) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	apiClient := r.p.client
	var state DbAuditLoggingConfig
	getIDsFromDbAuditLoggingState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	clusterId := state.ClusterID.ValueString()
	configId := state.ConfigID.ValueString()
	clusterName := state.ClusterName.ValueString()

	errMsg := fmt.Sprintf("Failed to disable DB Audit Logging on cluster %s", clusterName)

	_, _, err := apiClient.ClusterApi.RemoveDbAuditLogExporterConfig(ctx, accountId, projectId, clusterId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError(errMsg, GetApiErrorDetails(err))
		return
	}

	retryPolicy := clusterRunningTaskRetryPolicy(ctx, accountId, projectId, clusterId, apiClient)
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
func (r resourceDbAuditLogging) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the cluster_id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("cluster_id"), req, resp)
}
