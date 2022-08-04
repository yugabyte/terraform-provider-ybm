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

type resourceVPCType struct{}

func (r resourceVPCType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a VPC in YugabyteDB Managed.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this VPC belongs to.",
				Type:        types.StringType,
				Required:    true,
			},
			"project_id": {
				Description: "The ID of the project this VPC belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"vpc_id": {
				Description: "The ID of the VPC. Filled automatically on creating a VPC. Used to get a specific VPC.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"name": {
				Description: "The description of the VPC.",
				Type:        types.StringType,
				Required:    true,
			},
			"cloud": {
				Description: "The cloud on which the VPC has to be created.",
				Type:        types.StringType,
				Required:    true,
			},
			"global_cidr": {
				Description: "The global CIDR of the VPC (allowed only on GCP).",
				Type:        types.StringType,
				Optional:    true,
			},
			"region_cidr_info": {
				Description: "The CIDR information for all the regions for the VPC.",
				Optional:    true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"region": {
						Type:     types.StringType,
						Optional: true,
					},
					"cidr": {
						Type:     types.StringType,
						Optional: true,
					},
				}),
			},
		},
	}, nil
}

func (r resourceVPCType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceVPC{
		p: *(p.(*provider)),
	}, nil
}

type resourceVPC struct {
	p provider
}

func getVPCPlan(ctx context.Context, plan tfsdk.Plan, vpc *VPC) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &vpc)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("account_id"), &vpc.AccountID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("vpc_id"), &vpc.VPCID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("name"), &vpc.Name)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cloud"), &vpc.Cloud)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("global_cidr"), &vpc.GlobalCIDR)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("region_cidr_info"), &vpc.RegionCIDRInfo)...)

	return diags
}

// Create vpc
func (r resourceVPC) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider hasn't been configured before apply, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan VPC
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(getVPCPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client
	accountId := plan.AccountID.Value

	if (!plan.VPCID.Unknown && !plan.VPCID.Null) || plan.VPCID.Value != "" {
		resp.Diagnostics.AddError(
			"VPC ID provided when creating a vpc",
			"The vpc_id field was provided even though a new vpc is being created. Make sure this field is not in the provider on creation.",
		)
		return
	}
	projectId, getProjectOK, message := getProjectId(accountId, apiClient)
	if !getProjectOK {
		resp.Diagnostics.AddError("Could not get project ID", message)
		return
	}

	globalCIDRPresent := false
	if (!plan.GlobalCIDR.Unknown && !plan.GlobalCIDR.Null) || plan.GlobalCIDR.Value != "" {
		globalCIDRPresent = true
	}

	regionCIDRInfoPresent := false
	if plan.RegionCIDRInfo != nil {
		regionCIDRInfoPresent = true
	}

	// Exactly one parameter amongst Global CIDR and Region CIDR Info must be present
	// Simulating XOR by comparing boolean values
	if globalCIDRPresent == regionCIDRInfoPresent {
		resp.Diagnostics.AddError(
			"Problem with CIDR input",
			"Please provide exactly one parameter amongst Global CIDR and Region CIDR Info. Please don't provide both or none.",
		)
		return
	}

	vpcName := plan.Name.Value
	cloud := plan.Cloud.Value

	if cloud != "GCP" && globalCIDRPresent {
		resp.Diagnostics.AddError(
			"Global CIDR not allowed",
			"Global CIDR is allowed only for GCP.",
		)
		return
	}

	regionMap := map[string]int{}
	vpcRegionSpec := []openapiclient.VpcRegionSpec{}

	if regionCIDRInfoPresent {
		for index, info := range plan.RegionCIDRInfo {
			region := info.Region.Value
			cidr := info.CIDR.Value
			spec := *openapiclient.NewVpcRegionSpecWithDefaults()
			regionMap[region] = index
			spec.SetRegion(region)
			spec.SetCidr(cidr)
			vpcRegionSpec = append(vpcRegionSpec, spec)
		}
		// Ensure distinct regions are specified in the region CIDR info
		// This is not handled in the API yet
		if len(regionMap) != len(plan.RegionCIDRInfo) {
			resp.Diagnostics.AddError(
				"Invalid Spec",
				"Please ensure the regions are unique.",
			)
			return
		}
	}

	vpcSpec := *openapiclient.NewSingleTenantVpcSpec(openapiclient.CloudEnum(cloud), vpcName, vpcRegionSpec)
	if globalCIDRPresent {
		vpcSpec.SetParentCidr(plan.GlobalCIDR.Value)
	}
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
