/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"fmt"
	"net/http/httputil"
	"strconv"

	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
		Description: `The resource to create a YugabyteDB cluster. Use this resource to create both 
single- and multi-region clusters. You can also use this resource to bind allow lists to the cluster 
being created; restore previously taken backups to the cluster being created; 
and modify the backup schedule of the cluster being created.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this cluster belongs to. To be provided if there are multiple accounts associated with the user.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this cluster belongs to.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},

			"cluster_id": {
				Description: "The ID of the cluster. Created automatically when a cluster is created. Used to get a specific cluster.",
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
				Description: "The type of the cluster. SYNCHRONOUS or GEO_PARTITIONED",
				Type:        types.StringType,
				Required:    true,
			},
			"cloud_type": {
				Description: "The cloud provider where the cluster is deployed: AWS or GCP.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("AWS", "GCP")},
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
			"backup_schedules": {
				Optional: true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{

					"state": {

						Description: "The state of the backup schedule. Used to pause or resume the backup schedule. Valid values are ACTIVE or PAUSED.",
						Type:        types.StringType,
						Computed:    true,
						Optional:    true,
					},

					"cron_expression": {
						Description: "The cron expression for the backup schedule",
						Type:        types.StringType,
						Computed:    true,
						Optional:    true,
					},

					"time_interval_in_days": {
						Description: "The time interval in days for the backup schedule.",
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
						Description: "The ID of the backup schedule. Created automatically when the backup schedule is created. Used to get a specific backup schedule.",
						Type:        types.StringType,
						Computed:    true,
						Optional:    true,
					},
				}),
			},
			"cluster_tier": {
				Description: "FREE (Sandbox) or PAID (Dedicated).",
				Type:        types.StringType,
				Required:    true,
				Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("FREE", "PAID")},
			},
			"fault_tolerance": {
				Description: "The fault tolerance of the cluster. NONE, NODE, ZONE or REGION.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("NONE", "NODE", "ZONE", "REGION")},
			},
			"cluster_allow_list_ids": {
				Description: "List of IDs of the allow lists assigned to the cluster.",
				Type: types.ListType{
					ElemType: types.StringType,
				},
				Optional: true,
			},
			"restore_backup_id": {
				Description: "The ID of the backup to be restored to the cluster.",
				Type:        types.StringType,
				Optional:    true,
			},
			"node_config": {
				Required: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"num_cores": {
						Description: "Number of CPU cores in the node.",
						Type:        types.Int64Type,
						Required:    true,
					},
					"disk_size_gb": {
						Description: "Disk size of the node.",
						Type:        types.Int64Type,
						Computed:    true,
						Optional:    true,
					},
				}),
			},
			"credentials": {
				Description: `Credentials to be used by the database. Please provide 'username' and 'password' 
(which would be used in common for both YSQL and YCQL) OR all of 'ysql_username',
'ysql_password', 'ycql_username' and 'ycql_password' but not a mix of both.`,
				Required: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"username": {
						Description: "The username to be used for both YSQL and YCQL.",
						Type:        types.StringType,
						Optional:    true,
					},
					"password": {
						Description: "The password to be used for both YSQL and YCQL. Note that this will be stored in the state file.",
						Type:        types.StringType,
						Optional:    true,
					},
					"ysql_username": {
						Description: "YSQL username for the database.",
						Type:        types.StringType,
						Optional:    true,
					},
					"ysql_password": {
						Description: "YSQL password for the database. Note that this will be stored in the state file.",
						Type:        types.StringType,
						Optional:    true,
						Sensitive:   true,
					},
					"ycql_username": {
						Description: "YCQL username for the database.",
						Type:        types.StringType,
						Optional:    true,
					},
					"ycql_password": {
						Description: "YCQL password for the database. Note that this will be stored in the state file.",
						Type:        types.StringType,
						Optional:    true,
						Sensitive:   true,
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
			"database_track": {
				Description: "The track of the database. Stable or Preview.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"desired_state": {
				Description: "The desired state of the database, Active or Paused. This parameter can be used to pause/resume a cluster.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []tfsdk.AttributeValidator{
					// Validate string value must be "Active" or "Paused"
					stringvalidator.OneOf([]string{"Active", "Paused"}...),
				},
			},
			"cluster_endpoints": {
				Description: "The endpoints used to connect to the cluster by region.",
				Type: types.MapType{
					ElemType: types.StringType,
				},
				Optional: true,
				Computed: true,
			},
			"cluster_certificate": {
				Description: "The certificate used to connect to the cluster.",
				Type:        types.StringType,
				Computed:    true,
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

func EditBackupSchedule(ctx context.Context, backupScheduleStruct BackupScheduleInfo, scheduleId string, backupDes string, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) error {
	if backupScheduleStruct.State.Value != "" && backupScheduleStruct.RetentionPeriodInDays.Value != 0 {
		backupRetentionPeriodInDays := int32(backupScheduleStruct.RetentionPeriodInDays.Value)
		backupDescription := backupDes
		backupSpec := *openapiclient.NewBackupSpec(clusterId)
		backupSpec.Description = &backupDescription
		backupSpec.RetentionPeriodInDays = &backupRetentionPeriodInDays
		scheduleSpec := *openapiclient.NewScheduleSpec(openapiclient.ScheduleStateEnum(backupScheduleStruct.State.Value))
		if backupScheduleStruct.TimeIntervalInDays.Value != 0 {
			timeIntervalInDays := int32(backupScheduleStruct.TimeIntervalInDays.Value)
			scheduleSpec.TimeIntervalInDays = &timeIntervalInDays

		}
		if backupScheduleStruct.CronExpression.Value != "" {
			cronExp := backupScheduleStruct.CronExpression.Value
			scheduleSpec.SetCronExpression(cronExp)
		}
		if backupScheduleStruct.TimeIntervalInDays.Value != 0 && backupScheduleStruct.CronExpression.Value != "" {
			return errors.New("Unable to create custom backup schedule. You can't pass both the cron expression and time interval in days.")
		}
		backupScheduleSpec := *openapiclient.NewBackupScheduleSpec(backupSpec, scheduleSpec)
		_, res, err := apiClient.BackupApi.ModifyBackupSchedule(ctx, accountId, projectId, scheduleId).BackupScheduleSpec(backupScheduleSpec).Execute()
		if err != nil {
			b, _ := httputil.DumpResponse(res, true)
			return errors.New("Unable to modify the backup schedule. " + string(b))
		}
	}
	return nil
}

func createClusterSpec(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, plan Cluster, clusterExists bool) (clusterSpec *openapiclient.ClusterSpec, clusterSpecOK bool, errorMessage string) {

	var diskSizeGb int32
	var diskSizeOK bool
	var memoryMb int32
	var memoryOK bool
	var trackId string
	var trackName string
	var trackIdOK bool
	var message string

	networking := *openapiclient.NewNetworkingWithDefaults()

	// Compute track ID for database version
	softwareInfo := *openapiclient.NewSoftwareInfoWithDefaults()
	if !plan.DatabaseTrack.Unknown {
		trackName = plan.DatabaseTrack.Value
		trackId, trackIdOK, message = getTrackId(ctx, apiClient, accountId, trackName)
		if !trackIdOK {
			return nil, false, message
		}
		softwareInfo.SetTrackId(trackId)
	}

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
		info.SetIsDefault(false)
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

	cloud := plan.CloudType.Value
	tier := plan.ClusterTier.Value
	numCores := int32(plan.NodeConfig.NumCores.Value)
	memoryMb, memoryOK, message = getMemoryFromInstanceType(ctx, apiClient, accountId, cloud, tier, region, numCores)
	if !memoryOK {
		return nil, false, message
	}

	// Computing the default disk size if it is not provided
	if !plan.NodeConfig.DiskSizeGb.IsUnknown() {
		diskSizeGb = int32(plan.NodeConfig.DiskSizeGb.Value)
	} else {
		diskSizeGb, diskSizeOK, message = getDiskSizeFromInstanceType(ctx, apiClient, accountId, cloud, tier, region, numCores)
		if !diskSizeOK {
			return nil, false, message
		}
	}

	// This is to support a redundant value in the API.
	// Needs to be removed once API cleans it up.
	isProduction := true
	if plan.ClusterTier.Value == "FREE" {
		isProduction = false
	}

	clusterInfo := *openapiclient.NewClusterInfo(
		openapiclient.ClusterTier(tier),
		int32(totalNodes),
		openapiclient.ClusterFaultTolerance(plan.FaultTolerance.Value),
		*openapiclient.NewClusterNodeInfo(
			numCores,
			memoryMb,
			diskSizeGb,
		),
		isProduction,
	)

	clusterInfo.SetClusterType(openapiclient.ClusterType(clusterType))
	if clusterExists {
		cluster_version, _ := strconv.Atoi(plan.ClusterVersion.Value)
		clusterInfo.SetVersion(int32(cluster_version))
	}

	clusterSpec = openapiclient.NewClusterSpec(
		plan.ClusterName.Value,
		*openapiclient.NewCloudInfo(
			openapiclient.CloudEnum(plan.CloudType.Value),
			region),
		clusterInfo,
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
	diags.Append(plan.GetAttribute(ctx, path.Root("database_track"), &cluster.DatabaseTrack)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("desired_state"), &cluster.DesiredState)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("node_config"), &cluster.NodeConfig)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("backup_schedules"), &cluster.BackupSchedules)...)

	return diags
}

// fills account, project, cluster ID from state
func getIDsFromState(ctx context.Context, state tfsdk.State, cluster *Cluster) {
	state.GetAttribute(ctx, path.Root("account_id"), &cluster.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &cluster.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &cluster.ClusterID)
	state.GetAttribute(ctx, path.Root("desired_state"), &cluster.DesiredState)
	state.GetAttribute(ctx, path.Root("cluster_allow_list_ids"), &cluster.ClusterAllowListIDs)
	state.GetAttribute(ctx, path.Root("cluster_region_info"), &cluster.ClusterRegionInfo)
	state.GetAttribute(ctx, path.Root("backup_schedules"), &cluster.BackupSchedules)

}

func validateCredentials(credentials Credentials) bool {

	commonCredentialsProvided := !credentials.Username.IsNull() && !credentials.Password.IsNull()
	commonCredentialsNotProvided := credentials.Username.IsNull() && credentials.Password.IsNull()
	ysqlCredentialsProvided := !credentials.YSQLUsername.IsNull() && !credentials.YSQLPassword.IsNull()
	ysqlCredentialsNotProvided := credentials.YSQLUsername.IsNull() && credentials.YSQLPassword.IsNull()
	ycqlCredentialsProvided := !credentials.YCQLUsername.IsNull() && !credentials.YCQLPassword.IsNull()
	ycqlCredentialsNotProvided := credentials.YCQLUsername.IsNull() && credentials.YCQLPassword.IsNull()

	if (commonCredentialsProvided && ysqlCredentialsNotProvided && ycqlCredentialsNotProvided) ||
		(ysqlCredentialsProvided && ycqlCredentialsProvided && commonCredentialsNotProvided) {
		return true
	}

	return false

}

// Create a new resource
func (r resourceCluster) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan Cluster
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Cluster Resource: Error on Get Plan")
		return
	}

	if !validateCredentials(plan.Credentials) {
		resp.Diagnostics.AddError("Invalid credentials", `Please provide 'username' and 'password' 
(which would be used in common for both YSQL and YCQL) OR all of 'ysql_username',
'ysql_password', 'ycql_username' and 'ycql_password' but not a mix of both.`)
		return
	}

	if !plan.NodeConfig.DiskSizeGb.IsUnknown() && !isDiskSizeValid(plan.ClusterTier.Value, plan.NodeConfig.DiskSizeGb.Value) {
		resp.Diagnostics.AddError("Invalid disk size", "The disk size for a paid cluster must be at least 50 GB.")
		return
	}

	backupId := ""
	apiClient := r.p.client
	if !plan.AccountID.Null && !plan.AccountID.Unknown {
		accountId = plan.AccountID.Value
	} else {
		accountId, getAccountOK, message = getAccountId(ctx, apiClient)
		if !getAccountOK {
			resp.Diagnostics.AddError("Unable to get account ID", message)
			return
		}
	}

	if (!plan.ClusterID.Unknown && !plan.ClusterID.Null) || plan.ClusterID.Value != "" {
		resp.Diagnostics.AddError(
			"Cluster ID provided for new cluster",
			"The cluster_id was provided even though a new cluster is being created. Do not include this field in the provider when creating a cluster.",
		)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	clusterSpec, clusterOK, message := createClusterSpec(ctx, apiClient, accountId, plan, false)
	if !clusterOK {
		resp.Diagnostics.AddError("Unable to create cluster spec", message)
		return
	}

	credentials := openapiclient.NewCreateClusterRequestDbCredentials()
	if plan.Credentials.Username.IsNull() {
		credentials.SetYsql(*openapiclient.NewDBCredentials(plan.Credentials.YSQLPassword.Value, plan.Credentials.YSQLUsername.Value))
		credentials.SetYcql(*openapiclient.NewDBCredentials(plan.Credentials.YCQLPassword.Value, plan.Credentials.YCQLUsername.Value))
	} else {
		credentials.SetYsql(*openapiclient.NewDBCredentials(plan.Credentials.Password.Value, plan.Credentials.Username.Value))
		credentials.SetYcql(*openapiclient.NewDBCredentials(plan.Credentials.Password.Value, plan.Credentials.Username.Value))
	}

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

	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if clusterState == "Active" || clusterState == "Create Failed" {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get cluster state: " + message))
		}
		return retry.RetryableError(errors.New("The cluster creation is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create cluster ", "The operation timed out waiting for cluster creation.")
		return
	}

	// Backup_schedule

	scheduleResp, r1, err := apiClient.BackupApi.ListBackupSchedules(ctx, accountId, projectId).EntityId(clusterId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch the backup schedule for the cluster "+r1.Status, "Try again")
		return
	}
	list := scheduleResp.GetData()
	scheduleId := list[0].GetInfo().Id
	params := list[0].GetInfo().TaskParams
	var backUpSchedules []BackupScheduleInfo
	if plan.BackupSchedules != nil && len(plan.BackupSchedules) > 0 {
		if len(plan.BackupSchedules) > 1 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "More than one schedules were passed")
			return
		}

		if plan.BackupSchedules[0].State.Value != "" && plan.BackupSchedules[0].RetentionPeriodInDays.Value == 0 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "Pass both state and retention period in days ")
			return
		}
		if plan.BackupSchedules[0].State.Value == "" && plan.BackupSchedules[0].RetentionPeriodInDays.Value != 0 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "Pass both state and retention period in days ")
			return
		}
		description := params["description"].(string)
		//Edit Backup Schedule
		err = EditBackupSchedule(ctx, plan.BackupSchedules[0], scheduleId, description, accountId, projectId, clusterId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error duing store: ", err.Error())
			return
		}
		backupScheduleStruct := BackupScheduleInfo{
			ScheduleID: types.String{Value: scheduleId},
		}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)
	}
	if plan.BackupSchedules != nil && len(plan.BackupSchedules) == 0 {
		backupScheduleStruct := BackupScheduleInfo{}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)

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
			resp.Diagnostics.AddError("Unable to assign allow list to cluster", string(b))
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
			resp.Diagnostics.AddError("Error during store: ", err.Error())
			return
		}
	}

	regions := []string{}
	for _, regionInfo := range plan.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.Value)
	}

	// Pause the cluster if the desired state is set to 'Paused'
	if !plan.DesiredState.Unknown && plan.DesiredState.Value == "Paused" {
		err := pauseCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Cluster Creation Failed: ", err.Error())
		}
	}

	cluster, readOK, message := resourceClusterRead(ctx, accountId, projectId, clusterId, backUpSchedules, regions, allowListProvided, allowListIDs, false, apiClient)

	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster ", message)
		return
	}

	// set credentials for cluster (not returned by read api)
	if plan.Credentials.Username.IsNull() {
		cluster.Credentials.YSQLUsername.Value = plan.Credentials.YSQLUsername.Value
		cluster.Credentials.YSQLPassword.Value = plan.Credentials.YSQLPassword.Value
		cluster.Credentials.YCQLUsername.Value = plan.Credentials.YCQLUsername.Value
		cluster.Credentials.YCQLPassword.Value = plan.Credentials.YCQLPassword.Value
		cluster.Credentials.Username.Null = true
		cluster.Credentials.Password.Null = true
	} else {
		// common credentials have been used
		cluster.Credentials.Username.Value = plan.Credentials.Username.Value
		cluster.Credentials.Password.Value = plan.Credentials.Password.Value
		cluster.Credentials.YSQLUsername.Null = true
		cluster.Credentials.YSQLPassword.Null = true
		cluster.Credentials.YCQLUsername.Null = true
		cluster.Credentials.YCQLPassword.Null = true
	}

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

func pauseCluster(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (err error) {

	_, response, err := apiClient.ClusterApi.PauseCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		if len(string(b)) > 10000 {
			return errors.New("Could not pause the cluster. " + string(b)[:10000])
		}
		return errors.New("Could not pause the cluster. " + string(b))

	}

	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if clusterState == "Paused" {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get cluster state: " + message))
		}
		return retry.RetryableError(errors.New("The cluster is being paused."))
	})

	if err != nil {
		return errors.New("Unable to pause cluster. " + "The operation timed out waiting to pause the cluster.")
	}

	return nil

}

func resumeCluster(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (err error) {

	_, response, err := apiClient.ClusterApi.ResumeCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		if len(string(b)) > 10000 {
			return errors.New("Could not resume the cluster. " + string(b)[:10000])
		}
		return errors.New("Could not resume the cluster. " + string(b))
	}

	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if clusterState == "Active" {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get cluster state: " + message))
		}
		return retry.RetryableError(errors.New("The cluster is being resumed."))
	})

	if err != nil {
		return errors.New("Unable to resume cluster. " + "The operation timed out waiting to resume the cluster.")
	}

	return nil

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

	var backUpSchedules []BackupScheduleInfo
	if state.BackupSchedules != nil && len(state.BackupSchedules) > 0 {
		backUpSchedules = append(backUpSchedules, state.BackupSchedules[0])
	}
	cluster, readOK, message := resourceClusterRead(ctx, state.AccountID.Value, state.ProjectID.Value, state.ClusterID.Value, backUpSchedules, regions, allowListProvided, allowListIDs, false, r.p.client)

	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster", message)
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

func resourceClusterRead(ctx context.Context, accountId string, projectId string, clusterId string, backUpSchedules []BackupScheduleInfo, regions []string, allowListProvided bool, inputAllowListIDs []string, readOnly bool, apiClient *openapiclient.APIClient) (cluster Cluster, readOK bool, errorMessage string) {
	clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		return cluster, false, string(b)
	}

	if len(backUpSchedules) > 0 {
		//Below if is used for handling empty array edge case
		if backUpSchedules[0].ScheduleID.Value == "" {
			backupScheduleInfo := make([]BackupScheduleInfo, 0)
			cluster.BackupSchedules = backupScheduleInfo
		}
		if backUpSchedules[0].ScheduleID.Value != "" {
			backupScheduleResp, res, err := apiClient.BackupApi.GetBackupSchedule(context.Background(), accountId, projectId, backUpSchedules[0].ScheduleID.Value).Execute()
			if err != nil {
				b, _ := httputil.DumpResponse(res, true)
				return cluster, false, string(b)
			}
			// fill all fields of schema except credentials - credentials are not returned by api call

			params := backupScheduleResp.Data.Info.GetTaskParams()
			backupScheduleInfo := make([]BackupScheduleInfo, 1)
			backupScheduleStruct := BackupScheduleInfo{

				State:                 types.String{Value: string(backupScheduleResp.Data.Spec.GetState())},
				CronExpression:        types.String{Value: backupScheduleResp.Data.Spec.GetCronExpression()},
				BackupDescription:     types.String{Value: params["description"].(string)},
				RetentionPeriodInDays: types.Int64{Value: int64(params["retention_period_in_days"].(float64))},
				TimeIntervalInDays:    types.Int64{Value: int64(backupScheduleResp.Data.Spec.GetTimeIntervalInDays())},
				ScheduleID:            types.String{Value: backUpSchedules[0].ScheduleID.Value},
			}
			backupScheduleInfo[0] = backupScheduleStruct
			cluster.BackupSchedules = backupScheduleInfo
		}
	}

	// fill all fields of schema except credentials - credentials are not returned by api call
	cluster.AccountID.Value = accountId
	cluster.ProjectID.Value = projectId
	cluster.ClusterID.Value = clusterId
	cluster.ClusterName.Value = clusterResp.Data.Spec.Name
	cluster.DesiredState.Value = clusterResp.Data.Info.GetState()
	cluster.CloudType.Value = string(clusterResp.Data.Spec.CloudInfo.Code)
	cluster.ClusterType.Value = string(*clusterResp.Data.Spec.ClusterInfo.ClusterType)
	cluster.ClusterTier.Value = string(clusterResp.Data.Spec.ClusterInfo.ClusterTier)
	cluster.ClusterVersion.Value = strconv.Itoa(int(clusterResp.Data.Spec.ClusterInfo.GetVersion()))

	// set database track name
	trackId := clusterResp.Data.Spec.SoftwareInfo.GetTrackId()
	trackName, trackNameOK, message := getTrackName(ctx, apiClient, accountId, trackId)
	if !trackNameOK {
		return cluster, false, message
	}
	cluster.DatabaseTrack.Value = trackName

	cluster.FaultTolerance.Value = string(clusterResp.Data.Spec.ClusterInfo.FaultTolerance)
	cluster.NodeConfig.NumCores.Value = int64(clusterResp.Data.Spec.ClusterInfo.NodeInfo.NumCores)
	cluster.NodeConfig.DiskSizeGb.Value = int64(clusterResp.Data.Spec.ClusterInfo.NodeInfo.DiskSizeGb)

	cluster.ClusterInfo.State.Value = clusterResp.Data.Info.GetState()
	cluster.ClusterInfo.SoftwareVersion.Value = clusterResp.Data.Info.GetSoftwareVersion()
	cluster.ClusterInfo.CreatedTime.Value = clusterResp.Data.Info.Metadata.GetCreatedOn()
	cluster.ClusterInfo.UpdatedTime.Value = clusterResp.Data.Info.Metadata.GetUpdatedOn()

	// Cluster endpoints
	clusterEndpoints := types.Map{}
	clusterEndpoints.Elems = make(map[string]attr.Value)
	clusterEndpoints.ElemType = types.StringType
	for key, val := range clusterResp.Data.Info.Endpoints {
		clusterEndpoints.Elems[key] = types.String{Value: val}
	}
	cluster.ClusterEndpoints = clusterEndpoints

	// Cluster certificate
	certResponse, certHttpResp, err := apiClient.ClusterApi.GetConnectionCertificate(context.Background()).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(certHttpResp, true)
		return cluster, false, string(b)
	}
	cluster.ClusterCertificate.Value = *certResponse.Data

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
		return errors.New("Unable to restore backup to cluster: " + string(b))
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
			return retry.RetryableError(errors.New("Unable to get restore state: " + message))
		}
		return retry.RetryableError(errors.New("The backup restore is in progress"))
	})

	if err != nil {
		return errors.New("Unable to restore backup to the cluster: The operation timed out waiting for backup restore.")
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

	if !plan.NodeConfig.DiskSizeGb.IsUnknown() && !isDiskSizeValid(plan.ClusterTier.Value, plan.NodeConfig.DiskSizeGb.Value) {
		resp.Diagnostics.AddError("Invalid disk size", "The disk size for a paid cluster must be at least 50 GB.")
		return
	}

	apiClient := r.p.client
	var state Cluster
	getIDsFromState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value

	// Resume the cluster if the desired state is set to 'Active' and it is paused currently
	if state.DesiredState.Value == "Paused" && (plan.DesiredState.Unknown || plan.DesiredState.Value == "Active") {
		// Resume the cluster
		err := resumeCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Cluster update failed: ", err.Error())
			return
		}
	}

	scheduleResp, r1, err := apiClient.BackupApi.ListBackupSchedules(ctx, accountId, projectId).EntityId(clusterId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Could not fetch the backup schedule for the cluster "+r1.Status, "Try again")
		return
	}
	list := scheduleResp.GetData()
	scheduleId := list[0].GetInfo().Id
	params := list[0].GetInfo().TaskParams
	backupDescription := params["description"].(string)

	clusterSpec, clusterOK, message := createClusterSpec(ctx, apiClient, accountId, plan, false)
	if !clusterOK {
		resp.Diagnostics.AddError("Unable to create cluster specification ", message)
		return
	}

	clusterVersion, versionOK, message := getClusterVersion(accountId, projectId, clusterId, apiClient)
	if !versionOK {
		resp.Diagnostics.AddError("Unable to get cluster version ", message)
		return
	}
	clusterSpec.ClusterInfo.SetVersion(int32(clusterVersion))

	_, response, err := apiClient.ClusterApi.EditCluster(context.Background(), accountId, projectId, clusterId).ClusterSpec(*clusterSpec).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		if len(string(b)) > 10000 {
			resp.Diagnostics.AddError("Unable to edit cluster. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows: ",
				string(b)[:10000])
			return
		}
		resp.Diagnostics.AddError("Unable to edit cluster ", string(b))
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
			return retry.RetryableError(errors.New("Unable to get cluster state: " + message))
		}
		return retry.RetryableError(errors.New("Cluster creation in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create cluster", "The operation timed out waiting for cluster creation.")
		return
	}

	var backUpSchedules []BackupScheduleInfo
	if plan.BackupSchedules != nil && len(plan.BackupSchedules) > 0 {
		if len(plan.BackupSchedules) > 1 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "More than one schedules were passed")
			return
		}
		if plan.BackupSchedules[0].State.Value != "" && plan.BackupSchedules[0].RetentionPeriodInDays.Value == 0 {
			resp.Diagnostics.AddError("Unable to modify backup schedule", "You must provide both state and retention period in days.")
			return
		}
		if plan.BackupSchedules[0].State.Value == "" && plan.BackupSchedules[0].RetentionPeriodInDays.Value != 0 {
			resp.Diagnostics.AddError("Unable to modify backup schedule", "You must provide both state and retention period in days.")
			return
		}
		//Edit Backup Schedule
		err = EditBackupSchedule(ctx, plan.BackupSchedules[0], scheduleId, backupDescription, accountId, projectId, clusterId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error duing store: ", err.Error())
			return
		}
		backupScheduleStruct := BackupScheduleInfo{
			ScheduleID: types.String{Value: scheduleId},
		}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)
	}

	if plan.BackupSchedules != nil && len(plan.BackupSchedules) == 0 {
		backupScheduleStruct := BackupScheduleInfo{}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)

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
			resp.Diagnostics.AddError("Unable to assign allow list to cluster ", string(b))
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
			resp.Diagnostics.AddError("Error during store: ", err.Error())
			return
		}
	}

	// Pause the cluster if the desired state is set to 'Paused' and it is active currently
	if state.DesiredState.Value == "Active" && (!plan.DesiredState.Unknown && plan.DesiredState.Value == "Paused") {
		// Pause the cluster
		err := pauseCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Cluster update failed: ", err.Error())
			return
		}
	}

	regions := []string{}
	for _, regionInfo := range plan.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.Value)
	}

	cluster, readOK, message := resourceClusterRead(ctx, accountId, projectId, clusterId, backUpSchedules, regions, allowListProvided, allowListIDs, false, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster ", message)
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
		resp.Diagnostics.AddError("Unable to delete the cluster ", string(b))
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
