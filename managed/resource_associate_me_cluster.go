package managed

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/schemavalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
	"golang.org/x/exp/slices"
)

type resourceAssociateMetricsExporterClusterType struct{}

type metricsExportStateDefaultPlanModifier struct{}

func (m metricsExportStateDefaultPlanModifier) Description(_ context.Context) string {
	return "When export_state is omitted from configuration, the planned value is Active."
}

func (m metricsExportStateDefaultPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m metricsExportStateDefaultPlanModifier) Modify(_ context.Context, req tfsdk.ModifyAttributePlanRequest, resp *tfsdk.ModifyAttributePlanResponse) {
	if req.AttributeConfig == nil || req.AttributeConfig.IsNull() {
		resp.AttributePlan = types.String{Value: "Active"}
	}
}

func (r resourceAssociateMetricsExporterClusterType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "Use this resource to assign an export configuration (created using the `ybm_integration` resource) to a cluster for the export of cluster metrics. When assigned, cluster metrics are exported to the sink defined in the export configuration.",
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "ID of the account this metrics export configuration belongs to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"project_id": {
				Description: "ID of the project this metrics export configuration belongs to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"cluster_id": {
				Description: "ID of the cluster with which this metrics export configuration will be associated.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
					tfsdk.RequiresReplace(),
				},
				Validators: []tfsdk.AttributeValidator{
					schemavalidator.ExactlyOneOf(path.MatchRoot("cluster_name")),
				},
			},
			"cluster_name": {
				Description: "Name of the cluster with which this metrics export configuration will be associated.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
					tfsdk.RequiresReplace(),
				},
				Validators: []tfsdk.AttributeValidator{
					schemavalidator.ExactlyOneOf(path.MatchRoot("cluster_id")),
				},
			},
			"config_id": {
				Description: "ID of the integration for this metrics export configuration.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []tfsdk.AttributeValidator{
					schemavalidator.ExactlyOneOf(path.MatchRoot("config_name")),
				},
			},
			"config_name": {
				Description: "Name of the integration for this metrics export configuration",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []tfsdk.AttributeValidator{
					schemavalidator.ExactlyOneOf(path.MatchRoot("config_id")),
				},
			},
			"export_state": {
				Description: "The desired metrics export state on the cluster. Valid values are `Active` and `Paused`. Defaults to `Active`.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					metricsExportStateDefaultPlanModifier{},
				},
				Validators: []tfsdk.AttributeValidator{
					stringvalidator.OneOf([]string{"Active", "Paused"}...),
				},
			},
		},
	}, nil
}

func (r resourceAssociateMetricsExporterClusterType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceAssociateMetricsExporterCluster{
		p: *(p.(*provider)),
	}, nil
}

type resourceAssociateMetricsExporterCluster struct {
	p provider
}

func getAssocMetricsExporterClusterPlan(ctx context.Context, plan tfsdk.Plan, as *AssociateMetricsExporterCluster) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("config_name"), &as.ConfigName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("config_id"), &as.ConfigID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &as.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_name"), &as.ClusterName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("export_state"), &as.ExportState)...)
	return diags
}

func getIDsFromAssocMetricsExporterClusterState(ctx context.Context, state tfsdk.State, as *AssociateMetricsExporterCluster) {
	state.GetAttribute(ctx, path.Root("account_id"), &as.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &as.ProjectID)
	state.GetAttribute(ctx, path.Root("config_id"), &as.ConfigID)
	state.GetAttribute(ctx, path.Root("config_name"), &as.ConfigName)
	state.GetAttribute(ctx, path.Root("cluster_id"), &as.ClusterID)
	state.GetAttribute(ctx, path.Root("cluster_name"), &as.ClusterName)
	state.GetAttribute(ctx, path.Root("export_state"), &as.ExportState)
}

func (r resourceAssociateMetricsExporterCluster) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}
	var plan AssociateMetricsExporterCluster
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getAssocMetricsExporterClusterPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the metrics exporter association")
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
		resp.Diagnostics.AddError("Unable to get the project ID", message)
		return
	}

	configID := plan.ConfigID.Value
	configName := plan.ConfigName.Value

	config, err := GetConfigByNameorID(accountId, projectId, configID, configName, apiClient)
	if err != nil {
		resp.Diagnostics.AddError("unable to associate metrics exporter to cluster", GetApiErrorDetails(err))
		return
	}
	clusterID := plan.ClusterID.Value
	clusterName := plan.ClusterName.Value
	cluster, err := GetClusterByNameorID(accountId, projectId, clusterID, clusterName, apiClient)
	if err != nil {
		resp.Diagnostics.AddError("unable to associate metrics exporter to cluster", GetApiErrorDetails(err))
		return
	}

	metricsExporterClusterConfigSpec := openapiclient.NewMetricsExporterClusterConfigurationSpec(config.GetInfo().Id)
	_, _, err = apiClient.MetricsExporterConfigApi.AddMetricsExporterConfigToCluster(ctx, accountId, projectId, cluster.Info.Id).MetricsExporterClusterConfigurationSpec(*metricsExporterClusterConfigSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("unable to associate metrics exporter to cluster", GetApiErrorDetails(err))
		return
	}

	err = waitForMetricsExporterTask(ctx, accountId, projectId, cluster.Info.Id, openapiclient.TASKTYPEENUM_CONFIGURE_METRICS_EXPORTER, "metrics exporter is being associated to the cluster", apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Unable to associate metrics exporter to the cluster", err.Error())
		return
	}

	if strings.EqualFold(plannedMetricsExportState(plan.ExportState), "Paused") {
		err = stopMetricsExporterAndWait(ctx, accountId, projectId, cluster.Info.Id, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Unable to pause metrics export on the cluster", err.Error())
			return
		}
	}

	as, readOK, message := resourceAssociateMetricsExporterClusterRead(accountId, projectId, cluster.Info.Id, config.GetInfo().Id, cluster.GetSpec().Name, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of metrics exporter association ", message)
		return
	}

	diags := resp.State.Set(ctx, &as)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

func (r resourceAssociateMetricsExporterCluster) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state AssociateMetricsExporterCluster

	getIDsFromAssocMetricsExporterClusterState(ctx, req.State, &state)
	apiClient := r.p.client
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	configId := state.ConfigID.Value
	clusterId := state.ClusterID.Value
	clusterName := state.ClusterName.Value

	as, readOK, message := resourceAssociateMetricsExporterClusterRead(accountId, projectId, clusterId, configId, clusterName, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of metrics exporter association ", message)
		return
	}

	diags := resp.State.Set(ctx, &as)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

func resourceAssociateMetricsExporterClusterRead(accountId string, projectId string, clusterID string, configID string, clusterName string, apiClient *openapiclient.APIClient) (as AssociateMetricsExporterCluster, readOK bool, errorMessage string) {
	config, err := GetConfigByNameorID(accountId, projectId, configID, "", apiClient)
	if err != nil {
		return as, false, GetApiErrorDetails(err)
	}

	as.AccountID.Value = accountId
	as.ProjectID.Value = projectId
	as.ConfigID.Value = configID
	as.ConfigName.Value = config.GetSpec().Name

	if slices.Contains(config.GetInfo().ClusterIds, clusterID) {
		as.ClusterID.Value = clusterID
		// In case the cluster name change
		cluster, err := GetClusterByNameorID(accountId, projectId, clusterID, clusterName, apiClient)
		if err != nil {
			return as, false, GetApiErrorDetails(err)
		}
		as.ClusterName.Value = cluster.Spec.Name
		as.ExportState.Value = metricsExportStateFromCluster(cluster)
	}
	return as, true, ""
}

func (r resourceAssociateMetricsExporterCluster) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	apiClient := r.p.client
	var state AssociateMetricsExporterCluster
	getIDsFromAssocMetricsExporterClusterState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value

	_, err := apiClient.MetricsExporterConfigApi.RemoveMetricsExporterConfigFromCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("unable to deassociate metrics exporter to cluster", GetApiErrorDetails(err))
		return
	}

	err = waitForMetricsExporterTask(ctx, accountId, projectId, clusterId, openapiclient.TASKTYPEENUM_REMOVE_METRICS_EXPORTER, "metrics exporter is being de-associated from the cluster", apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Unable to de-associated metrics exporter from the cluster", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)

}

func (r resourceAssociateMetricsExporterCluster) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan AssociateMetricsExporterCluster
	resp.Diagnostics.Append(getAssocMetricsExporterClusterPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the metrics exporter association")
		return
	}

	apiClient := r.p.client
	var state AssociateMetricsExporterCluster
	getIDsFromAssocMetricsExporterClusterState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	clusterName := state.ClusterName.Value

	if plan.ClusterID != state.ClusterID {
		resp.Diagnostics.AddError("'cluster_id' cannot be updated", "'cluster_id' cannot be updated when updating metrics exporter cluster association")
	}

	configID, configName := plannedMetricsExportConfig(plan)
	config, err := GetConfigByNameorID(accountId, projectId, configID, configName, apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update metrics exporter association", GetApiErrorDetails(err))
		return
	}

	desiredExportState := plannedMetricsExportState(plan.ExportState)
	configChanged := config.GetInfo().Id != state.ConfigID.Value
	shouldPause := strings.EqualFold(desiredExportState, "Paused") && !strings.EqualFold(state.ExportState.Value, "Paused")
	shouldResume := strings.EqualFold(desiredExportState, "Active") && strings.EqualFold(state.ExportState.Value, "Paused")

	if configChanged || shouldResume {
		err = configureMetricsExporterAndWait(ctx, accountId, projectId, clusterId, config.GetInfo().Id, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Unable to update metrics exporter association for cluster", err.Error())
			return
		}
	}

	// Re-configure activates export; pause again when the desired state is still Paused.
	if strings.EqualFold(desiredExportState, "Paused") && (shouldPause || configChanged) {
		err = stopMetricsExporterAndWait(ctx, accountId, projectId, clusterId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Unable to pause metrics export on cluster", err.Error())
			return
		}
	}

	as, readOK, message := resourceAssociateMetricsExporterClusterRead(accountId, projectId, clusterId, config.GetInfo().Id, clusterName, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of metrics exporter association ", message)
		return
	}

	diags := resp.State.Set(ctx, &as)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Import API Key
func (r resourceAssociateMetricsExporterCluster) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Requires https://yugabyte.atlassian.net/browse/CLOUDGA-22221
	resp.Diagnostics.AddError("Import is not currently supported", "")
}

func plannedMetricsExportState(exportState types.String) string {
	if exportState.Null || exportState.Unknown || exportState.Value == "" {
		return "Active"
	}
	return exportState.Value
}

func plannedMetricsExportConfig(plan AssociateMetricsExporterCluster) (configID string, configName string) {
	if !plan.ConfigID.Null && !plan.ConfigID.Unknown {
		configID = plan.ConfigID.Value
	}
	if !plan.ConfigName.Null && !plan.ConfigName.Unknown {
		configName = plan.ConfigName.Value
	}
	return configID, configName
}

func metricsExportStateFromCluster(cluster *openapiclient.ClusterData) string {
	if cluster == nil {
		return "Active"
	}
	info := cluster.GetInfo()
	if !info.HasMetricsExporterInfo() {
		return "Active"
	}
	exporterInfo := info.GetMetricsExporterInfo()
	if exporterInfo.GetState() == openapiclient.CLUSTERMETRICSEXPORTERSTATUSENUM_STOPPED {
		return "Paused"
	}
	return "Active"
}

func configureMetricsExporterAndWait(ctx context.Context, accountId, projectId, clusterId, configID string, apiClient *openapiclient.APIClient) error {
	metricsExporterClusterConfigSpec := openapiclient.NewMetricsExporterClusterConfigurationSpec(configID)
	_, _, err := apiClient.MetricsExporterConfigApi.AddMetricsExporterConfigToCluster(ctx, accountId, projectId, clusterId).MetricsExporterClusterConfigurationSpec(*metricsExporterClusterConfigSpec).Execute()
	if err != nil {
		return errors.New(GetApiErrorDetails(err))
	}
	return waitForMetricsExporterTask(ctx, accountId, projectId, clusterId, openapiclient.TASKTYPEENUM_CONFIGURE_METRICS_EXPORTER, "metrics exporter configuration is being updated", apiClient)
}

func stopMetricsExporterAndWait(ctx context.Context, accountId, projectId, clusterId string, apiClient *openapiclient.APIClient) error {
	_, err := apiClient.MetricsExporterConfigApi.StopMetricsExporter(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return errors.New(GetApiErrorDetails(err))
	}
	return waitForMetricsExporterTask(ctx, accountId, projectId, clusterId, openapiclient.TASKTYPEENUM_STOP_METRICS_EXPORTER, "metrics export is being paused", apiClient)
}

func waitForMetricsExporterTask(ctx context.Context, accountId, projectId, clusterId string, taskType openapiclient.TaskTypeEnum, inProgressMessage string, apiClient *openapiclient.APIClient) error {
	retryPolicy := clusterRunningTaskRetryPolicy(ctx, accountId, projectId, clusterId, apiClient)
	err := retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, taskType, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return retry.RetryableError(errors.New("Unable to check metrics exporter task status: " + message))
		}
		return retry.RetryableError(errors.New(inProgressMessage))
	})
	if err != nil {
		return fmt.Errorf("the operation timed out waiting for the metrics exporter task to complete: %w", err)
	}
	return nil
}

func getTaskState(accountId string, projectId string, entityId string, entityType openapiclient.EntityTypeEnum, taskType openapiclient.TaskTypeEnum, apiclient *openapiclient.APIClient, ctx context.Context) (state string, readOK bool, errorMessage string) {
	currentStatus := "UNKNOWN"
	apiRequest := apiclient.TaskApi.ListTasks(ctx, accountId).TaskType(taskType).ProjectId(projectId).EntityId(entityId).Limit(1)
	if len(entityType) > 0 {
		apiRequest.EntityType(entityType)
	}
	taskList, _, err := apiRequest.Execute()
	if err != nil {
		return "", false, GetApiErrorDetails(err)
	}

	if v, ok := taskList.GetDataOk(); ok && v != nil {
		c := taskList.GetData()
		if len(c) == 0 {
			tflog.Info(ctx, "No task found for this operation")
			return "TASK_NOT_FOUND", true, ""
		}

		if len(c) > 0 {
			if status, ok := c[0].GetInfoOk(); ok {
				currentStatus = status.GetState()
			}
		}
	}
	return currentStatus, true, ""
}
