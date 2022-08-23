package managed

import (
	"context"
	"errors"
	"net/http/httputil"

	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceReadReplicasType struct{}

func (r resourceReadReplicasType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create read replicas of a particular cluster. You can create multiple read replicas
		in different regions using a single resource.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this read replica belongs to. To be provided if there are multiple accounts associated with the user.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this read replica belongs to.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"read_replicas_info": {
				Required:    true,
				Description: "Information about multiple read replicas.",
				Attributes: tfsdk.SetNestedAttributes(map[string]tfsdk.Attribute{
					"cloud_type": {
						Description: "The cloud provider where the read replica is deployed: AWS or GCP. Default GCP.",
						Type:        types.StringType,
						Optional:    true,
						Computed:    true,
					},
					"num_nodes": {
						Description: "The number of nodes of the read replica.",
						Type:        types.Int64Type,
						Required:    true,
					},
					"num_replicas": {
						Description: "The number of replicas of the read replica.",
						Type:        types.Int64Type,
						Required:    true,
					},
					"region": {
						Description: "The region of the read replica.",
						Type:        types.StringType,
						Required:    true,
					},
					"vpc_id": {
						Description: "The ID of the VPC where the read replica is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
					"multi_zone": {
						Description: "Set whether to spread the nodes in this region across zones. Defaults to false.",
						Optional:    true,
						Type:        types.BoolType,
					},
					"node_config": {
						Required:    true,
						Description: "The node configuration of the read replica.",
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"num_cores": {
								Type:     types.Int64Type,
								Required: true,
							},
							"memory_mb": {
								Type:     types.Int64Type,
								Required: true,
							},
							"disk_size_gb": {
								Type:     types.Int64Type,
								Required: true,
							},
						}),
					},
					"endpoint": {
						Description: "The endpoint of the read replica. Created automatically when a read replica is created.",
						Computed:    true,
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"host": {
								Description: "The host of the endpoint.",
								Type:        types.StringType,
								Computed:    true,
							},
							"region": {
								Description: "The region of the endpoint.",
								Type:        types.StringType,
								Computed:    true,
							},
							"accessibility_type": {
								Description: "The accessibility type of the endpoint. Private or Public.",
								Type:        types.StringType,
								Computed:    true,
							},
						}),
					},
				}),
			},
			"primary_cluster_id": {
				Description: "The primary cluster ID for the read replica.",
				Type:        types.StringType,
				Required:    true,
			},
		},
	}, nil
}

func (r resourceReadReplicasType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceReadReplicas{
		p: *(p.(*provider)),
	}, nil
}

type resourceReadReplicas struct {
	p provider
}

func createReadReplicasSpec(ctx context.Context, plan ReadReplicas) (readReplicasSpec []openapiclient.ReadReplicaSpec) {

	readReplicasInfo := plan.ReadReplicasInfo

	for _, readReplica := range readReplicasInfo {
		clusterNodeInfo := *openapiclient.NewClusterNodeInfo(
			int32(readReplica.NodeConfig.DiskSizeGb.Value),
			int32(readReplica.NodeConfig.MemoryMb.Value),
			int32(readReplica.NodeConfig.NumCores.Value))

		placementInfo := *openapiclient.NewPlacementInfo(
			*openapiclient.NewCloudInfo(
				openapiclient.CloudEnum(readReplica.CloudType.Value),
				readReplica.Region.Value), int32(readReplica.NumNodes.Value))
		placementInfo.SetNumReplicas(int32(readReplica.NumReplicas.Value))
		placementInfo.SetVpcId(readReplica.VPCID.Value)

		multiZone := false
		if !readReplica.MultiZone.Null {
			multiZone = readReplica.MultiZone.Value
		}
		placementInfo.SetMultiZone(multiZone)

		readReplicasSpec = append(readReplicasSpec, *openapiclient.NewReadReplicaSpec(clusterNodeInfo, placementInfo))

	}
	return readReplicasSpec
}

func getReadReplicasPlan(ctx context.Context, plan tfsdk.Plan, readReplicas *ReadReplicas) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &cluster)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("account_id"), &readReplicas.AccountID)...)
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
func (r resourceReadReplicas) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

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
	if !plan.AccountID.Null && !plan.AccountID.Unknown {
		accountId = plan.AccountID.Value
	} else {
		accountId, getAccountOK, message = getAccountId(apiClient)
		if !getAccountOK {
			resp.Diagnostics.AddError("Unable to get account ID", message)
			return
		}
	}
	projectId, getProjectOK, message := getProjectId(accountId, apiClient)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}
	clusterId := plan.PrimaryClusterID.Value

	readReplicasSpec := createReadReplicasSpec(ctx, plan)

	_, response, err := apiClient.ReadReplicaApi.CreateReadReplica(context.Background(), accountId, projectId, clusterId).ReadReplicaSpec(readReplicasSpec).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		if len(string(b)) > 10000 {
			resp.Diagnostics.AddError("Unable to create read replicas. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows: ",
				string(b)[:10000])
			return
		}
		resp.Diagnostics.AddError("Unable to create read replicas ", string(b))
		return
	}

	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		primaryClusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if primaryClusterState == "Active" {
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

	readReplicas, readOK, message := resourceReadReplicasRead(accountId, projectId, clusterId, apiClient)
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
func (r resourceReadReplicas) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state ReadReplicas
	getIDsFromReadReplicasState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.PrimaryClusterID.Value

	readReplicas, readOK, message := resourceReadReplicasRead(accountId, projectId, clusterId, r.p.client)
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

func resourceReadReplicasRead(accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (readReplicas ReadReplicas, readOK bool, errorMessage string) {

	listReadReplicasResp, response, err := apiClient.ReadReplicaApi.ListReadReplicas(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		return readReplicas, false, string(b)
	}

	readReplicas.AccountID.Value = accountId
	readReplicas.ProjectID.Value = projectId
	readReplicas.PrimaryClusterID.Value = clusterId

	readReplicasInfo := []ReadReplicaInfo{}
	endpointsPtr, getEndpointsOk := listReadReplicasResp.Data.Info.GetEndpointsOk()
	endpoints := *endpointsPtr

	for index, readReplicaSpec := range listReadReplicasResp.Data.GetSpec() {

		readReplicaInfo := ReadReplicaInfo{}
		readReplicaInfo.CloudType.Value = string(readReplicaSpec.PlacementInfo.CloudInfo.GetCode())
		readReplicaInfo.NumNodes.Value = int64(readReplicaSpec.PlacementInfo.GetNumNodes())
		readReplicaInfo.NumReplicas.Value = int64(readReplicaSpec.PlacementInfo.GetNumReplicas())
		readReplicaInfo.Region.Value = readReplicaSpec.PlacementInfo.CloudInfo.GetRegion()
		readReplicaInfo.VPCID.Value = readReplicaSpec.PlacementInfo.GetVpcId()
		readReplicaInfo.NodeConfig.NumCores.Value = int64(readReplicaSpec.NodeInfo.NumCores)
		readReplicaInfo.NodeConfig.MemoryMb.Value = int64(readReplicaSpec.NodeInfo.MemoryMb)
		readReplicaInfo.NodeConfig.DiskSizeGb.Value = int64(readReplicaSpec.NodeInfo.DiskSizeGb)
		if getEndpointsOk {
			readReplicaInfo.Endpoint.Host.Value = endpoints[index].GetHost()
			readReplicaInfo.Endpoint.Region.Value = endpoints[index].GetRegion()
			readReplicaInfo.Endpoint.AccessibilityType.Value = string(endpoints[index].GetAccessibilityType())
		}
		readReplicasInfo = append(readReplicasInfo, readReplicaInfo)

	}

	readReplicas.ReadReplicasInfo = readReplicasInfo

	return readReplicas, true, ""
}

// Update read replicas
func (r resourceReadReplicas) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {

	resp.Diagnostics.AddError("Unable to update read replicas.", "Updating read replicas is not currently supported. Delete and recreate the provider.")
	return
}

// Delete read replicas
func (r resourceReadReplicas) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state ReadReplicas
	getIDsFromReadReplicasState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.PrimaryClusterID.Value

	apiClient := r.p.client

	response, err := apiClient.ReadReplicaApi.DeleteReadReplica(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Unable to list the read replicas", string(b))
		return
	}

	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		primaryClusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if primaryClusterState == "Active" {
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
func (r resourceReadReplicas) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
