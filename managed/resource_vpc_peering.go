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

	//"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceVPCPeeringType struct{}

func (r resourceVPCPeeringType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a VPC Peering in YugabyteDB Managed.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this VPC Peering belongs to.",
				Type:        types.StringType,
				Required:    true,
			},
			"project_id": {
				Description: "The ID of the project this VPC Peering belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"vpc_peering_id": {
				Description: "The ID of the VPC peering.",
				Type:        types.StringType,
				Computed:    true,
			},
			"name": {
				Description: "The name of the VPC peering.",
				Type:        types.StringType,
				Required:    true,
			},
			"yugabytedb_vpc_id": {
				Description: "The ID of the VPC where the YugabyteDB cluster is deployed.",
				Type:        types.StringType,
				Required:    true,
			},
			"application_vpc_info": {
				Description: "The information of the VPC in which the application is deployed.",
				Required:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"cloud": {
						Description: "The cloud on which the application is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
					"project": {
						Description: "The project on the cloud in which the application is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
					"region": {
						Description: "The region in the cloud where the application is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
					"vpc_id": {
						Description: "The ID of the VPC in which the application is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
					"cidr": {
						Description: "The CIDR of the VPC in which the application is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
				}),
			},
			"vpc_peering_state": {
				Description: "The state of the VPC peering.",
				Type:        types.StringType,
				Computed:    true,
			},
		},
	}, nil
}

func (r resourceVPCPeeringType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceVPCPeering{
		p: *(p.(*provider)),
	}, nil
}

type resourceVPCPeering struct {
	p provider
}

func getVPCPeeringPlan(ctx context.Context, plan tfsdk.Plan, vpcPeering *VPCPeering) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &vpc)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("account_id"), &vpcPeering.AccountID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("vpc_peering_id"), &vpcPeering.VPCPeeringID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("yugabytedb_vpc_id"), &vpcPeering.YugabyteDBVPCID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("name"), &vpcPeering.Name)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("application_vpc_info"), &vpcPeering.ApplicationVPCInfo)...)

	return diags
}

// Create vpc peering
func (r resourceVPCPeering) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider hasn't been configured before apply, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan VPCPeering
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(getVPCPeeringPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client
	accountId := plan.AccountID.Value

	if (!plan.VPCPeeringID.Unknown && !plan.VPCPeeringID.Null) || plan.VPCPeeringID.Value != "" {
		resp.Diagnostics.AddError(
			"VPC Peering ID provided when creating a vpc peering",
			"The vpc_peering_id field was provided even though a new vpc peering is being created. Make sure this field is not in the provider on creation.",
		)
		return
	}
	projectId, getProjectOK, message := getProjectId(accountId, apiClient)
	if !getProjectOK {
		resp.Diagnostics.AddError("Could not get project ID", message)
		return
	}

	vpcPeeringName := plan.Name.Value
	yugabyteDBVPCID := plan.YugabyteDBVPCID.Value
	applicationCloud := plan.ApplicationVPCInfo.Cloud.Value
	applicationProject := plan.ApplicationVPCInfo.Project.Value
	applicationRegion := plan.ApplicationVPCInfo.Region.Value
	applicationVPCID := plan.ApplicationVPCInfo.VPCID.Value
	applicationVPCCIDR := plan.ApplicationVPCInfo.CIDR.Value

	applicationVPCSpec := *openapiclient.NewCustomerVpcSpec(*openapiclient.NewVpcCloudInfo(openapiclient.CloudEnum(applicationCloud)), applicationProject, applicationVPCID)
	applicationVPCSpec.CloudInfo.SetRegion(applicationRegion)
	vpcPeeringSpec := *openapiclient.NewVpcPeeringSpec(applicationVPCSpec, yugabyteDBVPCID, vpcPeeringName)

	vpcRequest := *openapiclient.NewSingleTenantVpcRequest(vpcSpec)

	vpcResp, response, err := apiClient.NetworkApi.CreateVpc(ctx, accountId, projectId).SingleTenantVpcRequest(vpcRequest).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Could not create vpc", string(b))
		return
	}
	vpcId := vpcResp.Data.Info.Id

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		vpcResp, _, err := apiClient.NetworkApi.GetSingleTenantVpc(context.Background(), accountId, projectId, vpcId).Execute()
		if err == nil {
			if vpcResp.Data.Info.State == "ACTIVE" {
				return nil
			}
		}
		return retry.RetryableError(errors.New("The vpc creation didn't succeed yet"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not create vpc", "Timed out waiting for vpc creation to be successful.")
		return
	}

	vpc, readOK, message := resourceVPCRead(accountId, projectId, vpcId, regionMap, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Could not read the state of the vpc", message)
		return
	}
	if !globalCIDRPresent {
		vpc.GlobalCIDR.Null = true
	} else {
		vpc.RegionCIDRInfo = nil
	}

	diags = resp.State.Set(ctx, &vpc)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromVPCState(ctx context.Context, state tfsdk.State, vpc *VPC) {
	state.GetAttribute(ctx, path.Root("account_id"), &vpc.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &vpc.ProjectID)
	state.GetAttribute(ctx, path.Root("vpc_id"), &vpc.VPCID)
}

func resourceVPCRead(accountId string, projectId string, vpcId string, regionMap map[string]int, apiClient *openapiclient.APIClient) (vpc VPC, readOK bool, errorMessage string) {
	vpcResp, response, err := apiClient.NetworkApi.GetSingleTenantVpc(context.Background(), accountId, projectId, vpcId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		return vpc, false, string(b)
	}

	vpc.AccountID.Value = accountId
	vpc.ProjectID.Value = projectId
	vpc.VPCID.Value = vpcId

	vpc.Name.Value = vpcResp.Data.Spec.GetName()
	vpc.Cloud.Value = string(vpcResp.Data.Spec.GetCloud())
	vpc.GlobalCIDR.Value = vpcResp.Data.Spec.GetParentCidr()

	if len(regionMap) > 0 {
		regionInfo := make([]VPCRegionInfo, len(regionMap))
		for _, info := range vpcResp.Data.Spec.GetRegionSpecs() {
			region := info.GetRegion()
			cidr := info.GetCidr()
			index := regionMap[region]
			regionInfo[index] = VPCRegionInfo{
				Region: types.String{Value: region},
				CIDR:   types.String{Value: cidr},
			}
		}
		vpc.RegionCIDRInfo = regionInfo
	}

	return vpc, true, ""
}

// Read vpc
func (r resourceVPC) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state VPC
	getIDsFromVPCState(ctx, req.State, &state)

	regionCIDRInfoPresent := false
	if state.RegionCIDRInfo != nil {
		regionCIDRInfoPresent = true
	}
	regionMap := map[string]int{}
	if regionCIDRInfoPresent {
		for index, info := range state.RegionCIDRInfo {
			region := info.Region.Value
			regionMap[region] = index
		}
	}

	vpc, readOK, message := resourceVPCRead(state.AccountID.Value, state.ProjectID.Value, state.VPCID.Value, regionMap, r.p.client)
	if !readOK {
		resp.Diagnostics.AddError("Could not read the state of the vpc", message)
		return
	}

	diags := resp.State.Set(ctx, &vpc)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update vpc
func (r resourceVPC) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {

	resp.Diagnostics.AddError("Could not update vpc.", "Updating a vpc is not supported yet. Please delete and recreate.")
	return

}

// Delete vpc
func (r resourceVPC) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state VPC
	getIDsFromVPCState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	vpcId := state.VPCID.Value

	apiClient := r.p.client

	response, err := apiClient.NetworkApi.DeleteVpc(context.Background(), accountId, projectId, vpcId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Could not delete the vpc", string(b))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(300*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		_, resp, err := apiClient.NetworkApi.GetSingleTenantVpc(context.Background(), accountId, projectId, vpcId).Execute()
		if err != nil {
			if resp.StatusCode == 404 {
				return nil
			}
		}
		return retry.RetryableError(errors.New("The vpc deletion didn't succeed yet"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not delete vpc", "Timed out waiting for vpc deletion to be successful.")
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import vpc
func (r resourceVPC) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
