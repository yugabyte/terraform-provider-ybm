package managed

import (
	"context"
	"errors"
	"fmt"
	"net/http/httputil"
	"strconv"

	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceClusterType struct{}

func (r resourceClusterType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a YugabyteDB cluster. This resource can be used to create both 
		single and multi-region clusters. The resource can also be used to bind allow lists to the cluster 
		being created and restore previously taken backups to the cluster being created. The resource can 
		also be used to modify the backup schedule of the cluster being created.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this cluster belongs to.",
				Type:        types.StringType,
				Required:    true,
			},
			"project_id": {
				Description: "The ID of the project this cluster belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},

			"cluster_id": {
				Description: "The id of the cluster. Filled automatically on creating a cluster. Use to get a specific cluster.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"cluster_name": {
				Description: "The name of the cluster.",
				Type:        types.StringType,
				Required:    true,
			},
			"cluster_type": {
				Description: "The type of the cluster.",
				Type:        types.StringType,
				Required:    true,
			},
			"cloud_type": {
				Description: "Which cloud the cluster is deployed in: AWS or GCP. Default GCP.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"cluster_region_info": {
				Required: true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"num_nodes": {
						Type:     types.Int64Type,
						Required: true,
					},
					"region": {
						Type:     types.StringType,
						Required: true,
					},
					"vpc_id": {
						Type:     types.StringType,
						Optional: true,
						Computed: true,
					},
				}),
			},
			"backup_schedule": {
				Optional: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{

					"state": {

						Description: "The state for  backup schedule. It is use to pause or resume the backup schedule. It can have value ACTIVE or PAUSED only.",
						Type:        types.StringType,
						Computed:    true,
						Optional:    true,
					},

					"cron_expression": {
						Description: "The cron expression for  backup schedule",
						Type:        types.StringType,
						Computed:    true,
						Optional:    true,
					},

					"time_interval_in_days": {
						Description: "The time interval in days for backup schedule.",
						Type:        types.Int64Type,
						Computed:    true,
						Optional:    true,
					},

					"retention_period_in_days": {
						Description: "The retention period of the backup schedule.",
						Type:        types.Int64Type,
						Computed:    true,
						Optional:    true,
					},

					"backup_description": {
						Description: "The description of the backup schedule.",
						Type:        types.StringType,
						Computed:    true,
						Optional:    true,
					},

					"schedule_id": {
						Description: "The id of the backup schedule. Filled automatically on creating a backup schedule. Used to get a specific backup schedule.",
						Type:        types.StringType,
						Computed:    true,
						Optional:    true,
					},
				}),
			},

			"cluster_tier": {
				Description: "FREE or PAID.",
				Type:        types.StringType,
				Required:    true,
			},
			"fault_tolerance": {
				Description: "The fault tolerance of the cluster.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"cluster_allow_list_ids": {
				Description: "The list of IDs of allow lists associated with the cluster.",
				Type: types.ListType{
					ElemType: types.StringType,
				},
				Optional: true,
			},
			"restore_backup_id": {
				Description: "The backup ID to be restored to the cluster.",
				Type:        types.StringType,
				Optional:    true,
			},
			"node_config": {
				Required: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"num_cores": {
						Type:     types.Int64Type,
						Optional: true,
						Computed: true,
					},
					"memory_mb": {
						Type:     types.Int64Type,
						Optional: true,
						Computed: true,
					},
					"disk_size_gb": {
						Type:     types.Int64Type,
						Optional: true,
						Computed: true,
					},
				}),
			},
			"credentials": {
				Required: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"ysql_username": {
						Type:     types.StringType,
						Required: true,
					},
					"ysql_password": {
						Type:      types.StringType,
						Required:  true,
						Sensitive: true,
					},
					"ycql_username": {
						Type:     types.StringType,
						Required: true,
					},
					"ycql_password": {
						Type:      types.StringType,
						Required:  true,
						Sensitive: true,
					},
				}),
			},
			"cluster_info": {
				Computed: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"state": {
						Type:     types.StringType,
						Computed: true,
					},
					"software_version": {
						Type:     types.StringType,
						Computed: true,
					},
					"created_time": {
						Type:     types.StringType,
						Computed: true,
					},
					"updated_time": {
						Type:     types.StringType,
						Computed: true,
					},
				}),
			},
			"cluster_version": {
				Type:     types.StringType,
				Computed: true,
			},
		},
	}, nil
}

func (r resourceClusterType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceCluster{
		p: *(p.(*provider)),
	}, nil
}

type resourceCluster struct {
	p provider
}

func EditBackupSchedule(ctx context.Context, plan Cluster, scheduleId string, backupDes string, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) error {
	if plan.BackupSchedule.State.Value != "" && plan.BackupSchedule.RetentionPeriodInDays.Value != 0 {
		backupRetentionPeriodInDays := int32(plan.BackupSchedule.RetentionPeriodInDays.Value)
		backupDescription := backupDes
		backupSpec := *openapiclient.NewBackupSpec(clusterId)
		backupSpec.Description = &backupDescription
		backupSpec.RetentionPeriodInDays = &backupRetentionPeriodInDays
		scheduleSpec := *openapiclient.NewScheduleSpec(openapiclient.ScheduleStateEnum(plan.BackupSchedule.State.Value))
		if plan.BackupSchedule.TimeIntervalInDays.Value != 0 {
			timeIntervalInDays := int32(plan.BackupSchedule.TimeIntervalInDays.Value)
			scheduleSpec.TimeIntervalInDays = &timeIntervalInDays

		}
		if plan.BackupSchedule.CronExpression.Value != "" {
			cronExp := plan.BackupSchedule.CronExpression.Value
			scheduleSpec.SetCronExpression(cronExp)
		}
		if plan.BackupSchedule.TimeIntervalInDays.Value != 0 && plan.BackupSchedule.CronExpression.Value != "" {
			return errors.New("Could not create custom backup schedule,connot pass cron expression and time interval in days both")
		}
		backupScheduleSpec := *openapiclient.NewBackupScheduleSpec(backupSpec, scheduleSpec)

		_, res, err := apiClient.BackupApi.ModifyBackupSchedule(ctx, accountId, projectId, scheduleId).BackupScheduleSpec(backupScheduleSpec).Execute()

		if err != nil {
			b, _ := httputil.DumpResponse(res, true)
			return errors.New("Could not create modify backup-schedule. " + string(b))
		}
	}
	return nil

}

func createClusterSpec(ctx context.Context, plan Cluster, clusterExists bool) (clusterSpec *openapiclient.ClusterSpec, clusterSpecOK bool, errorMessage string) {

	networking := *openapiclient.NewNetworkingWithDefaults()

	softwareInfo := *openapiclient.NewSoftwareInfoWithDefaults()

	clusterRegionInfo := []openapiclient.ClusterRegionInfo{}
	totalNodes := 0
	clusterType := plan.ClusterType.Value
	for _, regionInfo := range plan.ClusterRegionInfo {
		regionNodes := regionInfo.NumNodes.Value
		totalNodes += int(regionNodes)
		info := *openapiclient.NewClusterRegionInfo(
			*openapiclient.NewPlacementInfo(
				*openapiclient.NewCloudInfo(
					openapiclient.CloudEnum(plan.CloudType.Value),
					regionInfo.Region.Value), int32(regionNodes)),
		)
		if vpcID := regionInfo.VPCID.Value; vpcID != "" {
			info.PlacementInfo.SetVpcId(vpcID)
		}
		if clusterType == "SYNCHRONOUS" {
			info.PlacementInfo.SetMultiZone(false)
		}
		clusterRegionInfo = append(clusterRegionInfo, info)
	}

	// This is to populate region in top level cloud info
	region := ""
	regionCount := len(clusterRegionInfo)
	if regionCount > 0 {
		region = clusterRegionInfo[0].PlacementInfo.CloudInfo.Region
		if regionCount == 1 {
			clusterRegionInfo[0].SetIsDefault(true)
		}
	}

	// This is to support a redundant value in the API.
	// Needs to be removed once API cleans it up.
	isProduction := true

	clusterInfo := *openapiclient.NewClusterInfo(
		openapiclient.ClusterTier(plan.ClusterTier.Value),
		openapiclient.ClusterFaultTolerance(plan.FaultTolerance.Value),
		isProduction,
		*openapiclient.NewClusterNodeInfo(
			int32(plan.NodeConfig.DiskSizeGb.Value),
			int32(plan.NodeConfig.MemoryMb.Value),
			int32(plan.NodeConfig.NumCores.Value)),
		int32(totalNodes))

	clusterInfo.SetClusterType(openapiclient.ClusterType(clusterType))
	if clusterExists {
		cluster_version, _ := strconv.Atoi(plan.ClusterVersion.Value)
		clusterInfo.SetVersion(int32(cluster_version))
	}

	clusterSpec = openapiclient.NewClusterSpec(
		*openapiclient.NewCloudInfo(
			openapiclient.CloudEnum(plan.CloudType.Value),
			region),
		clusterInfo,
		plan.ClusterName.Value,
		networking,
		softwareInfo)

	clusterSpec.SetClusterRegionInfo(clusterRegionInfo)

	return clusterSpec, true, ""
}

func getPlan(ctx context.Context, plan tfsdk.Plan, cluster *Cluster) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &cluster)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("account_id"), &cluster.AccountID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &cluster.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_name"), &cluster.ClusterName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cloud_type"), &cluster.CloudType)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_type"), &cluster.ClusterType)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_region_info"), &cluster.ClusterRegionInfo)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("fault_tolerance"), &cluster.FaultTolerance)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_tier"), &cluster.ClusterTier)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_allow_list_ids"), &cluster.ClusterAllowListIDs)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("restore_backup_id"), &cluster.RestoreBackupID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("node_config"), &cluster.NodeConfig)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("backup_schedule"), &cluster.BackupSchedule)...)

	return diags
}

// fills account, project, cluster ID from state
func getIDsFromState(ctx context.Context, state tfsdk.State, cluster *Cluster) {
	state.GetAttribute(ctx, path.Root("account_id"), &cluster.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &cluster.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &cluster.ClusterID)
	state.GetAttribute(ctx, path.Root("cluster_allow_list_ids"), &cluster.ClusterAllowListIDs)
	state.GetAttribute(ctx, path.Root("cluster_region_info"), &cluster.ClusterRegionInfo)
	state.GetAttribute(ctx, path.Root("backup_schedule"), &cluster.BackupSchedule)

}

// Create a new resource
func (r resourceCluster) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider hasn't been configured before apply, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan Cluster
	resp.Diagnostics.Append(getPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Cluster Resource: Error on Get Plan")
		return
	}

	backupId := ""
	apiClient := r.p.client
	accountId := plan.AccountID.Value

	if (!plan.ClusterID.Unknown && !plan.ClusterID.Null) || plan.ClusterID.Value != "" {
		resp.Diagnostics.AddError(
			"Cluster ID provided when creating a cluster",
			"The cluster_id field was provided even though a new cluster is being created. Make sure this field is not in the provider on creation.",
		)
		return
	}

	projectId, getProjectOK, message := getProjectId(accountId, apiClient)
	if !getProjectOK {
		resp.Diagnostics.AddError("Could not get project ID", message)
		return
	}

	clusterSpec, clusterOK, message := createClusterSpec(ctx, plan, false)
	if !clusterOK {
		resp.Diagnostics.AddError("Could not create cluster spec", message)
		return
	}

	credentials := openapiclient.NewCreateClusterRequestDbCredentials()
	credentials.SetYsql(*openapiclient.NewDBCredentials(plan.Credentials.YSQLPassword.Value, plan.Credentials.YSQLUsername.Value))
	credentials.SetYcql(*openapiclient.NewDBCredentials(plan.Credentials.YCQLPassword.Value, plan.Credentials.YCQLUsername.Value))

	createClusterRequest := *openapiclient.NewCreateClusterRequest(*clusterSpec, *credentials)

	clusterResp, response, err := apiClient.ClusterApi.CreateCluster(ctx, accountId, projectId).CreateClusterRequest(createClusterRequest).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		if len(string(b)) > 10000 {
			resp.Diagnostics.AddError("Could not create cluster. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows:",
				string(b)[:10000])
			return
		}
		resp.Diagnostics.AddError("Could not create cluster", string(b))
		return
	}
	clusterId := clusterResp.Data.Info.Id

	// read status, wait for status to be don
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if clusterState == "Active" || clusterState == "Create Failed" {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Could not get cluster state: " + message))
		}
		return retry.RetryableError(errors.New("The cluster creation is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not create cluster ", "Timed out waiting for  cluster creation.")
		return
	}

	// Backup_schedule

	scheduleResp, r1, err := apiClient.BackupApi.ListBackupSchedules(ctx, accountId, projectId).EntityId(clusterId).Execute()

	if err != nil {
		resp.Diagnostics.AddError("Could not fetch the backup schedule for the cluster "+r1.Status, "Try again")
		return
	}
	if plan.BackupSchedule.State.Value != "" && plan.BackupSchedule.RetentionPeriodInDays.Value == 0 {
		resp.Diagnostics.AddError("Could not create custom backup schedule", "pass both state and retention period in days ")
		return
	}
	if plan.BackupSchedule.State.Value == "" && plan.BackupSchedule.RetentionPeriodInDays.Value != 0 {
		resp.Diagnostics.AddError("Could not create custom backup schedule", "pass both state and retention period in days ")
		return
	}
	list := scheduleResp.GetData()
	scheduleId := list[0].GetInfo().Id
	params := list[0].GetInfo().TaskParams
	//Edit Backup Schedule
	err = EditBackupSchedule(ctx, plan, scheduleId, params["description"].(string), accountId, projectId, clusterId, apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Error duing store: ", err.Error())
		return
	}
	allowListIDs := []string{}
	allowListProvided := false
	if plan.ClusterAllowListIDs != nil {
		for i := range plan.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, plan.ClusterAllowListIDs[i].Value)
		}

		tflog.Debug(ctx, fmt.Sprintf("Updating cluster with cluster ID %v with allow lists %v", clusterId, allowListIDs))

		_, response, err := apiClient.ClusterApi.EditClusterNetworkAllowLists(ctx, accountId, projectId, clusterId).RequestBody(allowListIDs).Execute()
		if err != nil {
			b, _ := httputil.DumpResponse(response, true)
			resp.Diagnostics.AddError("Could not assign allow list to cluster", string(b))
			return
		}
		allowListProvided = true
	}

	restoreRequired := false
	if (!plan.RestoreBackupID.Unknown && !plan.RestoreBackupID.Null) || plan.RestoreBackupID.Value != "" {
		restoreRequired = true
		backupId = plan.RestoreBackupID.Value
	}
	if restoreRequired {
		err = handleRestore(ctx, accountId, projectId, clusterId, backupId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error duing store: ", err.Error())
			return
		}
	}

	regions := []string{}
	for _, regionInfo := range plan.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.Value)
	}

	cluster, readOK, message := resourceClusterRead(accountId, projectId, clusterId, scheduleId, regions, allowListProvided, allowListIDs, false, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Could not read the state of the cluster", message)
		return
	}

	// set credentials for cluster (not returned by read api)
	cluster.Credentials.YSQLUsername.Value = plan.Credentials.YSQLUsername.Value
	cluster.Credentials.YSQLPassword.Value = plan.Credentials.YSQLPassword.Value
	cluster.Credentials.YCQLUsername.Value = plan.Credentials.YCQLUsername.Value
	cluster.Credentials.YCQLPassword.Value = plan.Credentials.YCQLPassword.Value
	// set restore backup id for cluster (not returned by read api)
	if restoreRequired {
		cluster.RestoreBackupID.Value = plan.RestoreBackupID.Value
	} else {
		cluster.RestoreBackupID.Null = true
	}

	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
func getClusterState(ctx context.Context, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (state string, readInfoOK bool, errorMessage string) {
	clusterResp, resp, err := apiClient.ClusterApi.GetCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(resp, true)
		return "", false, string(b)
	}

	return clusterResp.Data.Info.GetState(), true, ""
}

func getRestoreState(ctx context.Context, accountId string, projectId string, clusterId string, backupId string, restoreId string, apiClient *openapiclient.APIClient) (state string, readInfoOK bool, errorMessage string) {
	restoreResp, resp, err := apiClient.BackupApi.GetRestore(ctx, accountId, projectId, restoreId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(resp, true)
		return "", false, string(b)
	}
	//ListRestores(ctx, accountId, projectId).BackupId(backupId).ClusterId(clusterId).Execute()
	return string(restoreResp.Data.Info.GetState()), true, ""
}

// Read resource information
func (r resourceCluster) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state Cluster
	getIDsFromState(ctx, req.State, &state)

	allowListIDs := []string{}
	allowListProvided := false
	if state.ClusterAllowListIDs != nil {
		allowListProvided = true
		for i := range state.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, state.ClusterAllowListIDs[i].Value)
		}
	}

	regions := []string{}
	for _, regionInfo := range state.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.Value)
	}

	cluster, readOK, message := resourceClusterRead(state.AccountID.Value, state.ProjectID.Value, state.ClusterID.Value, state.BackupSchedule.ScheduleID.Value, regions, allowListProvided, allowListIDs, false, r.p.client)
	if !readOK {
		resp.Diagnostics.AddError("Could not read the state of the cluster", message)
		return
	}

	tflog.Debug(ctx, "Cluster Read: Allow List IDs read from API server", map[string]interface{}{
		"Allow List IDs": cluster.ClusterAllowListIDs})

	// set credentials for cluster (not returned by read api)
	req.State.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)
	// set restore backup id for cluster (not returned by read api)
	if !state.RestoreBackupID.Null {
		req.State.GetAttribute(ctx, path.Root("restore_backup_id"), &cluster.RestoreBackupID)
	}

	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getClusterRegionIndex(region string, readOnly bool, regionIndexMap map[string]int, localIndex int) (index int) {
	if readOnly {
		return localIndex
	}
	index, ok := regionIndexMap[region]
	if ok {
		return index
	}
	return -1
}

func resourceClusterRead(accountId string, projectId string, clusterId string, scheduleId string, regions []string, allowListProvided bool, inputAllowListIDs []string, readOnly bool, apiClient *openapiclient.APIClient) (cluster Cluster, readOK bool, errorMessage string) {
	clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		return cluster, false, string(b)
	}
	backupScheduleResp, res, err := apiClient.BackupApi.GetBackupSchedule(context.Background(), accountId, projectId, scheduleId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(res, true)
		return cluster, false, string(b)
	}
	// fill all fields of schema except credentials - credentials are not returned by api call
	cluster.AccountID.Value = accountId
	cluster.ProjectID.Value = projectId
	cluster.ClusterID.Value = clusterId
	cluster.BackupSchedule.ScheduleID.Value = scheduleId
	params := backupScheduleResp.Data.Info.GetTaskParams()
	cluster.BackupSchedule.BackupDescription.Value = params["description"].(string)
	cluster.BackupSchedule.RetentionPeriodInDays.Value = int64(params["retention_period_in_days"].(float64))
	cluster.BackupSchedule.TimeIntervalInDays.Value = int64(backupScheduleResp.Data.Spec.GetTimeIntervalInDays())
	cluster.BackupSchedule.CronExpression.Value = backupScheduleResp.Data.Spec.GetCronExpression()
	cluster.BackupSchedule.State.Value = string(backupScheduleResp.Data.Spec.GetState())
	cluster.ClusterName.Value = clusterResp.Data.Spec.Name
	cluster.CloudType.Value = string(clusterResp.Data.Spec.CloudInfo.Code)
	cluster.ClusterType.Value = string(*clusterResp.Data.Spec.ClusterInfo.ClusterType)
	cluster.ClusterTier.Value = string(clusterResp.Data.Spec.ClusterInfo.ClusterTier)
	cluster.ClusterVersion.Value = strconv.Itoa(int(clusterResp.Data.Spec.ClusterInfo.GetVersion()))

	cluster.FaultTolerance.Value = string(clusterResp.Data.Spec.ClusterInfo.FaultTolerance)
	cluster.NodeConfig.NumCores.Value = int64(clusterResp.Data.Spec.ClusterInfo.NodeInfo.NumCores)
	cluster.NodeConfig.MemoryMb.Value = int64(clusterResp.Data.Spec.ClusterInfo.NodeInfo.MemoryMb)
	cluster.NodeConfig.DiskSizeGb.Value = int64(clusterResp.Data.Spec.ClusterInfo.NodeInfo.DiskSizeGb)

	cluster.ClusterInfo.State.Value = clusterResp.Data.Info.GetState()
	cluster.ClusterInfo.SoftwareVersion.Value = clusterResp.Data.Info.GetSoftwareVersion()
	cluster.ClusterInfo.CreatedTime.Value = clusterResp.Data.Info.Metadata.GetCreatedOn()
	cluster.ClusterInfo.UpdatedTime.Value = clusterResp.Data.Info.Metadata.GetUpdatedOn()

	// This is being done to preserve order in the region list since an order mismatch is treated as state mismatch by Terraform
	regionIndexMap := map[string]int{}
	for index, region := range regions {
		regionIndexMap[region] = index
	}

	respClusterRegionInfo := clusterResp.Data.Spec.ClusterRegionInfo
	clusterRegionInfo := make([]RegionInfo, len(respClusterRegionInfo))
	for localIndex, info := range respClusterRegionInfo {
		region := info.PlacementInfo.CloudInfo.GetRegion()
		destIndex := getClusterRegionIndex(region, readOnly, regionIndexMap, localIndex)
		if destIndex < len(respClusterRegionInfo) {
			regionInfo := RegionInfo{
				Region:   types.String{Value: region},
				NumNodes: types.Int64{Value: int64(info.PlacementInfo.GetNumNodes())},
				VPCID:    types.String{Value: info.PlacementInfo.GetVpcId()},
			}
			clusterRegionInfo[destIndex] = regionInfo
		}
	}
	cluster.ClusterRegionInfo = clusterRegionInfo

	if allowListProvided {
		for {
			clusterAllowListMappingResp, response, err := apiClient.ClusterApi.ListClusterNetworkAllowLists(context.Background(), accountId, projectId, clusterId).Execute()
			if err != nil {
				b, _ := httputil.DumpResponse(response, true)
				return cluster, false, string(b)
			}
			allowListIDMap := map[string]bool{}
			allowListIDs := []types.String{}
			allowListStrings := []string{}
			// This is being to done to preserve order in the list since an order mismatch is treated as state mismatch by Terraform
			for _, elem := range clusterAllowListMappingResp.Data {
				allowListIDMap[elem.Info.Id] = true
			}
			if !readOnly {
				for _, elem := range inputAllowListIDs {
					if _, ok := allowListIDMap[elem]; ok {
						allowListStrings = append(allowListStrings, elem)
					}
				}
			}
			if readOnly {
				for _, elem := range clusterAllowListMappingResp.Data {
					allowListStrings = append(allowListStrings, elem.Info.Id)
				}
			}
			tflog.Debug(context.Background(), fmt.Sprintf("Input Allow List is %v, Server Allow List is %v", inputAllowListIDs, allowListStrings))
			//added len(inputAllowListIDs)==0 in if condition so that we can reuse the func resourceClusterRead in data_source_cluster_name.go.
			if areListsEqual(allowListStrings, inputAllowListIDs) || len(inputAllowListIDs) == 0 {
				for _, elem := range allowListStrings {
					allowListIDs = append(allowListIDs, types.String{Value: elem})
				}
				cluster.ClusterAllowListIDs = allowListIDs
				break
			}
			time.Sleep(1 * time.Second)
		}
	}

	return cluster, true, ""
}

func getClusterVersion(accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (version int, readOK bool, errorMessage string) {
	clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		return 0, false, string(b)
	}

	return int(clusterResp.Data.Spec.ClusterInfo.GetVersion()), true, ""
}

func handleRestore(ctx context.Context, accountId string, projectId string, clusterId string, backupId string, apiClient *openapiclient.APIClient) error {
	restoreSpec := *openapiclient.NewRestoreSpec()
	restoreSpec.SetBackupId(backupId)
	restoreSpec.SetClusterId(clusterId)
	tflog.Debug(ctx, fmt.Sprintf("Restoring to cluster with cluster ID %v the backup with backup ID %v", clusterId, backupId))

	restoreResp, response, err := apiClient.BackupApi.RestoreBackup(ctx, accountId, projectId).RestoreSpec(restoreSpec).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		return errors.New("Could not restore backup to cluster: " + string(b))
	}

	restoreId := *restoreResp.Data.Info.Id
	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		restoreState, readInfoOK, message := getRestoreState(ctx, accountId, projectId, clusterId, backupId, restoreId, apiClient)
		if readInfoOK {
			if restoreState == "SUCCEEDED" {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Could not get restore state: " + message))
		}
		return retry.RetryableError(errors.New("The backup restore is in progress"))
	})

	if err != nil {
		return errors.New("Could not restore backup to the cluster: Timed out waiting for backup restore.")
	}

	return nil
}

// Update resource
func (r resourceCluster) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan Cluster
	resp.Diagnostics.Append(getPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state Cluster
	getIDsFromState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	scheduleId := state.BackupSchedule.ScheduleID.Value
	backupDescription := state.BackupSchedule.BackupDescription.Value
	apiClient := r.p.client

	clusterSpec, clusterOK, message := createClusterSpec(ctx, plan, false)
	if !clusterOK {
		resp.Diagnostics.AddError("Could not create cluster spec", message)
		return
	}

	clusterVersion, versionOK, message := getClusterVersion(accountId, projectId, clusterId, apiClient)
	if !versionOK {
		resp.Diagnostics.AddError("Could not get cluster version", message)
		return
	}
	clusterSpec.ClusterInfo.SetVersion(int32(clusterVersion))

	_, response, err := apiClient.ClusterApi.EditCluster(context.Background(), accountId, projectId, clusterId).ClusterSpec(*clusterSpec).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		if len(string(b)) > 10000 {
			resp.Diagnostics.AddError("Could not edit cluster. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows:",
				string(b)[:10000])
			return
		}
		resp.Diagnostics.AddError("Could not edit cluster", string(b))
		return
	}

	// read status, wait for status to be don
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if clusterState == "Active" || clusterState == "Create Failed" {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Could not get cluster state: " + message))
		}
		return retry.RetryableError(errors.New("The cluster creation is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not create cluster ", "Timed out waiting for  cluster creation.")
		return
	}

	if plan.BackupSchedule.State.Value != "" && plan.BackupSchedule.RetentionPeriodInDays.Value == 0 {
		resp.Diagnostics.AddError("Could not modify custom backup schedule", "pass both state and retention period in days ")
		return
	}
	if plan.BackupSchedule.State.Value == "" && plan.BackupSchedule.RetentionPeriodInDays.Value != 0 {
		resp.Diagnostics.AddError("Could not modify custom backup schedule", "pass both state and retention period in days ")
		return
	}
	//Edit Backup Schedule
	err = EditBackupSchedule(ctx, plan, scheduleId, backupDescription, accountId, projectId, clusterId, apiClient)
	if err != nil {
		resp.Diagnostics.AddError("Error duing store: ", err.Error())
		return
	}
	allowListIDs := []string{}
	allowListProvided := false

	if plan.ClusterAllowListIDs != nil {
		for i := range plan.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, plan.ClusterAllowListIDs[i].Value)
		}

		_, response, err := apiClient.ClusterApi.EditClusterNetworkAllowLists(context.Background(), accountId, projectId, clusterId).RequestBody(allowListIDs).Execute()
		if err != nil {
			b, _ := httputil.DumpResponse(response, true)
			resp.Diagnostics.AddError("Could not assign allow list to cluster", string(b))
			return
		}
		allowListProvided = true
	}

	tflog.Debug(ctx, "Cluster Update: Details about allow list IDs", map[string]interface{}{
		"Allow List IDs":  allowListIDs,
		"Provided or Not": allowListProvided})

	restoreRequired := false
	backupId := ""
	if (!plan.RestoreBackupID.Unknown && !plan.RestoreBackupID.Null) || plan.RestoreBackupID.Value != "" {
		if state.RestoreBackupID.Value != plan.RestoreBackupID.Value {
			restoreRequired = true
		}
		backupId = plan.RestoreBackupID.Value
	}
	if restoreRequired {
		err = handleRestore(ctx, accountId, projectId, clusterId, backupId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error duing store: ", err.Error())
			return
		}
	}

	regions := []string{}
	for _, regionInfo := range plan.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.Value)
	}

	cluster, readOK, message := resourceClusterRead(accountId, projectId, clusterId, scheduleId, regions, allowListProvided, allowListIDs, false, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Could not read the state of the cluster", message)
		return
	}
	tflog.Debug(ctx, "Cluster Update: Allow list IDs read from API server ", map[string]interface{}{
		"Allow List IDs": cluster.ClusterAllowListIDs})

	// set credentials for cluster (not returned by read api)
	req.State.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)
	// set restore backup id for cluster (not returned by read api)
	if restoreRequired {
		cluster.RestoreBackupID.Value = plan.RestoreBackupID.Value
	} else {
		cluster.RestoreBackupID.Null = true
	}
	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete resource
func (r resourceCluster) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state Cluster
	getIDsFromState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value

	apiClient := r.p.client

	response, err := apiClient.ClusterApi.DeleteCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Could not delete the cluster", string(b))
		return
	}

	for {
		_, resp, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
		if err != nil {
			if resp.StatusCode == 404 {
				break
			}
		}
		time.Sleep(10 * time.Second)
	}

	resp.State.RemoveResource(ctx)
}

// Import resource
func (r resourceCluster) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
