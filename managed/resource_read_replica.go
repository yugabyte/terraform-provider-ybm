/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
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
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceReadReplicas) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The resource to create read replicas of a particular cluster. You can create multiple read replicas
		in different regions using a single resource.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this read replica belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this read replica belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"read_replicas_info": schema.ListNestedAttribute{
				Required:    true,
				Description: "Information about multiple read replicas.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"cloud_type": schema.StringAttribute{
							Description: "The cloud provider where the read replica is deployed: AWS, AZURE or GCP. Default GCP.",
							Optional:    true,
							Computed:    true,
							Validators:  []validator.String{stringvalidator.OneOf("AWS", "GCP", "AZURE")},
							PlanModifiers: []planmodifier.String{
								stringplanmodifier.UseNonNullStateForUnknown(),
							},
						},
						"num_nodes": schema.Int64Attribute{
							Description: "The number of nodes of the read replica.",
							Required:    true,
						},
						"num_replicas": schema.Int64Attribute{
							Description: "The number of replicas of the read replica.",
							Required:    true,
						},
						"region": schema.StringAttribute{
							Description: "The region of the read replica.",
							Required:    true,
						},
						"vpc_id": schema.StringAttribute{
							Description: "The ID of the VPC where the read replica is deployed.",
							Optional:    true,
							Computed:    true,
							Validators: []validator.String{
								stringvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("vpc_name")),
							},
							PlanModifiers: []planmodifier.String{
								stringplanmodifier.UseNonNullStateForUnknown(),
							},
						},
						"vpc_name": schema.StringAttribute{
							Description: "The name of the VPC where the read replica is deployed.",
							Optional:    true,
							Computed:    true,
							Validators: []validator.String{
								stringvalidator.ExactlyOneOf(path.MatchRelative().AtParent().AtName("vpc_name"),
									path.MatchRelative().AtParent().AtName("vpc_id"),
								),
							},
							PlanModifiers: []planmodifier.String{
								stringplanmodifier.UseNonNullStateForUnknown(),
							},
						},
						"multi_zone": schema.BoolAttribute{
							Description: "Set whether to spread the nodes in this region across zones. Defaults to true.",
							Optional:    true,
							Computed:    true,
							PlanModifiers: []planmodifier.Bool{
								boolplanmodifier.UseNonNullStateForUnknown(),
							},
						},
						"node_config": schema.SingleNestedAttribute{
							Required:    true,
							Description: "The node configuration of the read replica.",
							Attributes: map[string]schema.Attribute{
								"num_cores": schema.Int64Attribute{
									Required: true,
								},
								"disk_size_gb": schema.Int64Attribute{
									Required: true,
								},
								"disk_iops": schema.Int64Attribute{
									Optional: true,
									Computed: true,
									PlanModifiers: []planmodifier.Int64{
										int64planmodifier.UseNonNullStateForUnknown(),
									},
								},
							},
						},
						"endpoint": schema.StringAttribute{
							Description: "The endpoint of the read replica. Created automatically when a read replica is created.",
							Computed:    true,
							PlanModifiers: []planmodifier.String{
								stringplanmodifier.UseNonNullStateForUnknown(),
							},
						},
					},
				},
			},
			"primary_cluster_id": schema.StringAttribute{
				Description: "The primary cluster ID for the read replica.",
				Required:    true,
			},
		},
	}
}

func newResourceReadReplicas() resource.Resource {
	return &resourceReadReplicas{}
}

func (r *resourceReadReplicas) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_read_replicas"
}

func (r *resourceReadReplicas) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceReadReplicas struct {
	p provider
}

func createReadReplicasSpec(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, plan ReadReplicas) (readReplicasSpec []openapiclient.ReadReplicaSpec, readReplicaSpecOK bool, errorMessage string) {

	readReplicasInfo := plan.ReadReplicasInfo
	// Default tier "PAID" used for read replica. Tier is used to get memory from cpu cores using instance types.
	tier := "PAID"

	for _, readReplica := range readReplicasInfo {

		cloud := readReplica.CloudType.ValueString()
		region := readReplica.Region.ValueString()
		numCores := int32(readReplica.NodeConfig.NumCores.ValueInt64())
		memoryMb, memoryOK, message := getMemoryFromInstanceType(ctx, apiClient, accountId, cloud, tier, region, numCores)
		if !memoryOK {
			return nil, false, message
		}
		clusterNodeInfo := *openapiclient.NewOptionalClusterNodeInfo(
			numCores,
			memoryMb,
			int32(readReplica.NodeConfig.DiskSizeGb.ValueInt64()),
		)
		if !(readReplica.NodeConfig.DiskIops.IsUnknown() || readReplica.NodeConfig.DiskIops.IsNull()) && int32(readReplica.NodeConfig.DiskIops.ValueInt64()) > 0 {
			clusterNodeInfo.SetDiskIops(int32(readReplica.NodeConfig.DiskIops.ValueInt64()))
		}

		if vpcName := readReplica.VPCName.ValueString(); vpcName != "" {
			vpcData, err := getVPCByName(context.Background(), accountId, projectId, vpcName, apiClient)
			if err != nil {
				return nil, false, err.Error()
			}

			readReplica.VPCID = types.StringValue(vpcData.Info.Id)
		}

		placementInfo := *openapiclient.NewPlacementInfo(
			*openapiclient.NewCloudInfo(
				openapiclient.CloudEnum(cloud),
				region), int32(readReplica.NumNodes.ValueInt64()))
		placementInfo.SetNumReplicas(int32(readReplica.NumReplicas.ValueInt64()))
		placementInfo.SetVpcId(readReplica.VPCID.ValueString())

		multiZone := true
		if !(readReplica.MultiZone.IsUnknown() || readReplica.MultiZone.IsNull()) {
			multiZone = readReplica.MultiZone.ValueBool()
		}
		placementInfo.SetMultiZone(multiZone)

		currSpecEntry := *openapiclient.NewReadReplicaSpec(placementInfo)
		currSpecEntry.SetRegionNodeInfo(clusterNodeInfo)

		readReplicasSpec = append(readReplicasSpec, currSpecEntry)

	}
	return readReplicasSpec, true, ""
}

func getReadReplicasPlan(ctx context.Context, plan tfsdk.Plan, readReplicas *ReadReplicas) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &cluster)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("read_replicas_info"), &readReplicas.ReadReplicasInfo)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("primary_cluster_id"), &readReplicas.PrimaryClusterID)...)

	return diags
}

// fills account, project, read replica info from state
func getIDsFromReadReplicasState(ctx context.Context, state tfsdk.State, readReplicas *ReadReplicas) {
	state.GetAttribute(ctx, path.Root("account_id"), &readReplicas.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &readReplicas.ProjectID)
	state.GetAttribute(ctx, path.Root("primary_cluster_id"), &readReplicas.PrimaryClusterID)
	state.GetAttribute(ctx, path.Root("read_replicas_info"), &readReplicas.ReadReplicasInfo)
}

// Create a new resource
func (r resourceReadReplicas) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan ReadReplicas
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getReadReplicasPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Read Replicas Resource: Error on Get Plan")
		return
	}

	if plan.ReadReplicasInfo == nil {
		resp.Diagnostics.AddError(
			"No read replica specified",
			"You must specify at least one read replica.",
		)
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

	clusterId := plan.PrimaryClusterID.ValueString()

	readReplicasSpec, readReplicasOK, message := createReadReplicasSpec(ctx, apiClient, accountId, projectId, plan)
	if !readReplicasOK {
		resp.Diagnostics.AddError("Unable to create read replicas spec", message)
		return
	}

	_, response, err := apiClient.ReadReplicaApi.CreateReadReplica(context.Background(), accountId, projectId, clusterId).ReadReplicaSpec(readReplicasSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			resp.Diagnostics.AddError("Unable to create read replicas. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows: ",
				errMsg[:10000])
			return
		}
		resp.Diagnostics.AddError("Unable to create read replicas ", errMsg)
		return
	}

	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		primaryClusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(primaryClusterState, "Active") {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get the primary cluster's state: " + message))
		}
		return retry.RetryableError(errors.New("Read replica creation in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create read replicas ", "The operation timed out waiting for read replica creation.")
		return
	}

	regions := []string{}
	for _, readReplica := range plan.ReadReplicasInfo {
		regions = append(regions, readReplica.Region.ValueString())
	}

	readReplicas, readOK, message := resourceReadReplicasRead(ctx, clusterId, apiClient, regions)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the read replicas", message)
		return
	}

	diags := resp.State.Set(ctx, &readReplicas)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read read replica information
func (r resourceReadReplicas) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ReadReplicas
	getIDsFromReadReplicasState(ctx, req.State, &state)
	clusterId := state.PrimaryClusterID.ValueString()

	regions := []string{}
	for _, readReplica := range state.ReadReplicasInfo {
		regions = append(regions, readReplica.Region.ValueString())
	}

	readReplicas, readOK, message := resourceReadReplicasRead(ctx, clusterId, r.p.client, regions)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the read replica", message)
		return
	}

	diags := resp.State.Set(ctx, &readReplicas)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func resourceReadReplicasRead(ctx context.Context, clusterId string, apiClient *openapiclient.APIClient, planRegions []string) (readReplicas ReadReplicas, readOK bool, errorMessage string) {

	var accountId, projectId, message string
	var getAccountOK, getProjectOK bool

	accountId, getAccountOK, message = getAccountId(context.Background(), apiClient)
	if !getAccountOK {
		return readReplicas, false, fmt.Sprintf("unable to get account ID %s", message)
	}

	projectId, getProjectOK, message = getProjectId(context.Background(), apiClient, accountId)
	if !getProjectOK {
		return readReplicas, false, fmt.Sprintf("unable to get project ID %s", message)
	}

	listReadReplicasResp, response, err := apiClient.ReadReplicaApi.ListReadReplicas(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return readReplicas, false, errMsg
	}

	readReplicas.AccountID = types.StringValue(accountId)
	readReplicas.ProjectID = types.StringValue(projectId)
	readReplicas.PrimaryClusterID = types.StringValue(clusterId)

	endpointsPtr, getEndpointsOk := listReadReplicasResp.Data.Info.GetEndpointsOk()
	endpoints := *endpointsPtr

	// Preserve the order of items in the array since order mismatch is treated as state mismatch
	regionIndexMap := map[string]int{}
	for index, region := range planRegions {
		regionIndexMap[region] = index
	}

	readReplicasSpec := listReadReplicasResp.Data.GetSpec()
	readReplicasInfo := make([]ReadReplicaInfo, len(readReplicasSpec))

	for localIndex, readReplicaSpec := range readReplicasSpec {

		readReplicaInfo := ReadReplicaInfo{
			NodeConfig: NodeConfig{NumCores: types.Int64Value(0), DiskSizeGb: types.Int64Value(0), DiskIops: types.Int64Value(0)},
			Endpoint:   types.StringValue(""),
		}
		readReplicaInfo.CloudType = types.StringValue(string(readReplicaSpec.PlacementInfo.CloudInfo.GetCode()))
		readReplicaInfo.NumNodes = types.Int64Value(int64(readReplicaSpec.PlacementInfo.GetNumNodes()))
		readReplicaInfo.NumReplicas = types.Int64Value(int64(readReplicaSpec.PlacementInfo.GetNumReplicas()))
		readReplicaInfo.Region = types.StringValue(readReplicaSpec.PlacementInfo.CloudInfo.GetRegion())
		readReplicaInfo.VPCID = types.StringValue(readReplicaSpec.PlacementInfo.GetVpcId())
		vpcData, err := getVPCByID(context.Background(), accountId, projectId, readReplicaSpec.PlacementInfo.GetVpcId(), apiClient)
		if err != nil {
			return readReplicas, false, err.Error()
		}
		readReplicaInfo.VPCName = types.StringValue(vpcData.Spec.Name)
		readReplicaInfo.MultiZone = types.BoolValue(readReplicaSpec.PlacementInfo.GetMultiZone())

		// Use RegionNodeInfo (if nil, fall back to to-be-deprecated NodeInfo)
		regionNodeInfo := readReplicaSpec.RegionNodeInfo.Get()
		nodeInfo := readReplicaSpec.NodeInfo.Get()
		if regionNodeInfo != nil {
			readReplicaInfo.NodeConfig.NumCores = types.Int64Value(int64((*regionNodeInfo).NumCores))
			readReplicaInfo.NodeConfig.DiskSizeGb = types.Int64Value(int64((*regionNodeInfo).DiskSizeGb))
			if (*regionNodeInfo).DiskIops.Get() != nil {
				readReplicaInfo.NodeConfig.DiskIops = types.Int64Value(int64(*(*regionNodeInfo).DiskIops.Get()))
			}
		} else if nodeInfo != nil {
			readReplicaInfo.NodeConfig.NumCores = types.Int64Value(int64((*nodeInfo).NumCores))
			readReplicaInfo.NodeConfig.DiskSizeGb = types.Int64Value(int64((*nodeInfo).DiskSizeGb))
			if (*nodeInfo).DiskIops.Get() != nil {
				readReplicaInfo.NodeConfig.DiskIops = types.Int64Value(int64(*(*nodeInfo).DiskIops.Get()))
			}
		}

		if getEndpointsOk {
			readReplicaInfo.Endpoint = types.StringValue(endpoints[localIndex].GetHost())
		}

		destIndex := getClusterRegionIndex(readReplicaInfo.Region.ValueString(), regionIndexMap, localIndex)
		readReplicasInfo[destIndex] = readReplicaInfo

	}

	readReplicas.ReadReplicasInfo = readReplicasInfo

	return readReplicas, true, ""
}

// Update read replicas
func (r resourceReadReplicas) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	var state ReadReplicas
	getIDsFromReadReplicasState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	clusterId := state.PrimaryClusterID.ValueString()

	var plan ReadReplicas
	resp.Diagnostics.Append(getReadReplicasPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Read Replicas Resource: Error on Get Plan")
		return
	}
	if plan.ReadReplicasInfo == nil {
		resp.Diagnostics.AddError(
			"No read replica specified",
			"You must specify at least one read replica.",
		)
		return
	}

	apiClient := r.p.client

	readReplicasSpec, readReplicasOK, message := createReadReplicasSpec(ctx, apiClient, accountId, projectId, plan)
	if !readReplicasOK {
		resp.Diagnostics.AddError("Unable to create read replicas spec", message)
		return
	}

	tflog.Info(ctx, "Making call to update read replicas...")
	_, response, err := apiClient.ReadReplicaApi.EditReadReplicas(context.Background(), accountId, projectId, clusterId).ReadReplicaSpec(readReplicasSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			resp.Diagnostics.AddError("Unable to update read replicas. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows: ",
				errMsg[:10000])
			return
		}
		resp.Diagnostics.AddError("Unable to update read replicas ", errMsg)
		return
	}

	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		primaryClusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			tflog.Debug(ctx, "Read Replica current state = "+primaryClusterState)
			if strings.EqualFold(primaryClusterState, "Active") {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get the primary cluster's state: " + message))
		}
		return retry.RetryableError(errors.New("Read replica update in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to update read replicas ", "The operation timed out waiting for read replica update.")
		return
	}

	tflog.Debug(ctx, "Cluster is Active again, re-reading read-replica information.")

	regions := []string{}
	for _, readReplicaInfo := range plan.ReadReplicasInfo {
		regions = append(regions, readReplicaInfo.Region.ValueString())
	}

	readReplicas, readOK, message := resourceReadReplicasRead(ctx, clusterId, apiClient, regions)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the read replicas", message)
		return
	}

	diags := resp.State.Set(ctx, &readReplicas)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete read replicas
func (r resourceReadReplicas) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ReadReplicas
	getIDsFromReadReplicasState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	clusterId := state.PrimaryClusterID.ValueString()

	apiClient := r.p.client

	response, err := apiClient.ReadReplicaApi.DeleteReadReplica(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to delete the read replicas", errMsg)
		return
	}

	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		primaryClusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(primaryClusterState, "Active") {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get the primary cluster's state: " + message))
		}
		return retry.RetryableError(errors.New("Read replica deletion in progress."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to delete read replicas", "The operation timed out waiting for read replica deletion.")
		return
	}

	resp.State.RemoveResource(ctx)
}

// Import a read replica
func (r resourceReadReplicas) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the primary_cluster_id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("primary_cluster_id"), req, resp)
}
