/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceVPC) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The resource to create a VPC in YugabyteDB Aeon.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this VPC belongs to.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this VPC belongs to.",
				Computed:    true,
			},
			"vpc_id": schema.StringAttribute{
				Description: "The ID of the VPC. Created automatically when the VPC is created. Used to get a specific VPC.",
				Computed:    true,
				Optional:    true,
			},
			"external_vpc_id": schema.StringAttribute{
				Description: "The ID of the cloud provider(AWS/GCP/Azure) VPC where YugabyteDB Aeon resources are created",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "The description of the VPC.",
				Required:    true,
			},
			"cloud": schema.StringAttribute{
				Description: "The cloud provider (AWS, AZURE or GCP) where the VPC is to be created.",
				Required:    true,
			},
			"global_cidr": schema.StringAttribute{
				Description: "The global CIDR of the VPC (GCP only).",
				Optional:    true,
				Computed:    true,
			},
			"region_cidr_info": schema.ListNestedAttribute{
				Description: "The CIDR information for all the regions for the VPC.",
				Optional:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"region": schema.StringAttribute{
							Optional: true,
						},
						"cidr": schema.StringAttribute{
							Computed: true,
							Optional: true,
						},
					},
				},
			},
		},
	}
}

func newResourceVPC() resource.Resource {
	return &resourceVPC{}
}

func (r *resourceVPC) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc"
}

func (r *resourceVPC) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
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

	diags.Append(plan.GetAttribute(ctx, path.Root("vpc_id"), &vpc.VPCID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("name"), &vpc.Name)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cloud"), &vpc.Cloud)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("global_cidr"), &vpc.GlobalCIDR)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("region_cidr_info"), &vpc.RegionCIDRInfo)...)

	return diags
}

// Create vpc
func (r resourceVPC) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan VPC
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getVPCPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	if (!plan.VPCID.IsUnknown() && !plan.VPCID.IsNull()) || plan.VPCID.ValueString() != "" {
		resp.Diagnostics.AddError(
			"VPC ID provided for new VPC",
			"The vpc_id was provided even though a new VPC is being created. Do not include this field in the provider when creating a VPC.",
		)
		return
	}
	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get the project ID", message)
		return
	}

	globalCIDRPresent := false
	if (!plan.GlobalCIDR.IsUnknown() && !plan.GlobalCIDR.IsNull()) || plan.GlobalCIDR.ValueString() != "" {
		globalCIDRPresent = true
	}

	regionCIDRInfoPresent := false
	if plan.RegionCIDRInfo != nil {
		regionCIDRInfoPresent = true
	}

	vpcName := plan.Name.ValueString()
	cloud := plan.Cloud.ValueString()

	// Exactly one parameter amongst Global CIDR and Region CIDR Info must be present
	// Simulating XOR by comparing boolean values
	if globalCIDRPresent == regionCIDRInfoPresent {
		resp.Diagnostics.AddError(
			"Global and region CIDR details provided",
			"Specify either the global CIDR or the CIDR information for the regions. Don't provide both.",
		)
		return
	}
	if cloud != "GCP" && globalCIDRPresent {
		resp.Diagnostics.AddError(
			"Global CIDR specified",
			"Global CIDR only applies to GCP.",
		)
		return
	}

	if cloud == "AZURE" {
		if len(plan.RegionCIDRInfo) != 1 {
			resp.Diagnostics.AddError(
				"Unable to create VPC",
				"Only one region supported per Azure VPC.",
			)
			return
		}
		if (!plan.RegionCIDRInfo[0].CIDR.IsUnknown() && !plan.RegionCIDRInfo[0].CIDR.IsNull()) || plan.RegionCIDRInfo[0].CIDR.ValueString() != "" {
			resp.Diagnostics.AddError(
				"CIDR specifed",
				"CIDR are auto-assigned for AZURE. Please remove it",
			)
			return
		}
	}

	regionMap := map[string]int{}
	vpcRegionSpec := []openapiclient.VpcRegionSpec{}

	if regionCIDRInfoPresent {
		for index, info := range plan.RegionCIDRInfo {
			region := info.Region.ValueString()
			cidr := info.CIDR.ValueString()
			spec := *openapiclient.NewVpcRegionSpecWithDefaults()
			regionMap[region] = index
			spec.SetRegion(region)
			if cloud != "AZURE" {
				spec.SetCidr(cidr)
			}
			vpcRegionSpec = append(vpcRegionSpec, spec)
		}
		// Ensure distinct regions are specified in the region CIDR info
		// This is not handled in the API yet
		if len(regionMap) != len(plan.RegionCIDRInfo) {
			resp.Diagnostics.AddError(
				"Duplicate regions",
				"Ensure the regions are unique.",
			)
			return
		}
	}

	vpcSpec := *openapiclient.NewSingleTenantVpcSpec(vpcName, openapiclient.CloudEnum(cloud), vpcRegionSpec)
	if globalCIDRPresent {
		vpcSpec.SetParentCidr(plan.GlobalCIDR.ValueString())
	}
	vpcRequest := *openapiclient.NewSingleTenantVpcRequest(vpcSpec)

	vpcResp, response, err := apiClient.NetworkApi.CreateVpc(ctx, accountId, projectId).SingleTenantVpcRequest(vpcRequest).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create VPC", errMsg)
		return
	}
	vpcId := vpcResp.Data.Info.Id

	if recordCreatedResource(ctx, req.Plan, resp, "VPC", vpcId, map[string]attr.Value{"account_id": types.StringValue(accountId), "project_id": types.StringValue(projectId), "vpc_id": types.StringValue(vpcId)}) {
		defer warnIfCreateIncomplete(resp, "VPC", vpcId)
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		vpcResp, _, err := apiClient.NetworkApi.GetSingleTenantVpc(context.Background(), accountId, projectId, vpcId).Execute()
		if err == nil {
			if vpcResp.Data.Info.State == "ACTIVE" {
				return nil
			}
		}
		return retry.RetryableError(errors.New("VPC creation in progress."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create VPC", "The operation timed out waiting for VPC creation.")
		return
	}

	vpc, readOK, message := resourceVPCRead(vpcId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the VPC", message)
		return
	}

	if !globalCIDRPresent {
		vpc.GlobalCIDR = types.StringNull()
	} else {
		vpc.RegionCIDRInfo = nil
	}

	// We want to keep the order,  so if there are the same we use the one in the state
	if isEqual(vpc.RegionCIDRInfo, plan.RegionCIDRInfo) {
		if cloud != "AZURE" {
			vpc.RegionCIDRInfo = plan.RegionCIDRInfo
		}
	}
	diags := resp.State.Set(ctx, &vpc)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromVPCState(ctx context.Context, state tfsdk.State, vpc *VPC) {
	state.GetAttribute(ctx, path.Root("account_id"), &vpc.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &vpc.ProjectID)
	state.GetAttribute(ctx, path.Root("vpc_id"), &vpc.VPCID)
	state.GetAttribute(ctx, path.Root("region_cidr_info"), &vpc.RegionCIDRInfo)
	state.GetAttribute(ctx, path.Root("global_cidr"), &vpc.GlobalCIDR)
}

func resourceVPCRead(vpcId string, apiClient *openapiclient.APIClient) (vpc VPC, readOK bool, errorMessage string) {
	var accountId, projectId, message string
	var getAccountOK bool

	accountId, getAccountOK, message = getAccountId(context.Background(), apiClient)
	if !getAccountOK {
		return vpc, false, fmt.Sprintf("unable to get account ID %s", message)
	}

	projectId, getProjectOK, message := getProjectId(context.Background(), apiClient, accountId)
	if !getProjectOK {
		return vpc, false, fmt.Sprintf("unable to get project ID %s", message)
	}

	vpcResp, _, err := apiClient.NetworkApi.GetSingleTenantVpc(context.Background(), accountId, projectId, vpcId).Execute()
	if err != nil {
		return vpc, false, GetApiErrorDetails(err)
	}

	vpc.AccountID = types.StringValue(accountId)
	vpc.ProjectID = types.StringValue(projectId)
	vpc.VPCID = types.StringValue(vpcId)

	vpc.Name = types.StringValue(vpcResp.Data.Spec.GetName())
	vpc.Cloud = types.StringValue(string(vpcResp.Data.Spec.GetCloud()))
	vpc.ExternalVPCID = types.StringValue(vpcResp.Data.Info.GetExternalVpcId())
	if vpcResp.Data.Spec.GetParentCidr() != "" {
		vpc.GlobalCIDR = types.StringValue(vpcResp.Data.Spec.GetParentCidr())
	} else {
		vpc.GlobalCIDR = types.StringNull()
		if v, ok := vpcResp.Data.Spec.GetRegionSpecsOk(); ok {
			regionInfo := make([]VPCRegionInfo, len(*v))
			for index, info := range *v {
				region := info.GetRegion()
				cidr := info.GetCidr()
				regionInfo[index] = VPCRegionInfo{
					Region: types.StringValue(region),
					CIDR:   types.StringValue(cidr),
				}
			}
			sort.Slice(regionInfo, func(i, j int) bool {
				return string(regionInfo[i].Region.ValueString()) < string(regionInfo[j].Region.ValueString())
			})
			vpc.RegionCIDRInfo = regionInfo
		}

	}

	return vpc, true, ""
}

// Read vpc
func (r resourceVPC) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VPC
	getIDsFromVPCState(ctx, req.State, &state)

	vpc, readOK, message := resourceVPCRead(state.VPCID.ValueString(), r.p.client)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the VPC", message)
		return
	}
	// We want to keep the order,  so if there are the same we use the one in the state
	if isEqual(vpc.RegionCIDRInfo, state.RegionCIDRInfo) {
		vpc.RegionCIDRInfo = state.RegionCIDRInfo
	}
	diags := resp.State.Set(ctx, &vpc)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update vpc
func (r resourceVPC) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {

	resp.Diagnostics.AddError("Unable to update VPC.", "Updating VPCs is not currently supported. Delete and recreate the provider.")
	return

}

// Delete vpc
func (r resourceVPC) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VPC
	getIDsFromVPCState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	vpcId := state.VPCID.ValueString()

	apiClient := r.p.client

	response, err := apiClient.NetworkApi.DeleteVpc(context.Background(), accountId, projectId, vpcId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to delete the VPC", errMsg)
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(900*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		_, resp, err := apiClient.NetworkApi.GetSingleTenantVpc(context.Background(), accountId, projectId, vpcId).Execute()
		if err != nil {
			if resp.StatusCode == 404 {
				return nil
			}
		}
		return retry.RetryableError(errors.New("VPC deletion in progress."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to delete VPC", "The operation timed out waiting for the VPC to be deleted.")
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import vpc
func (r resourceVPC) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the vpc_id attribute
	resp.State.SetAttribute(ctx, path.Root("vpc_id"), req.ID)
}

func isEqual(x []VPCRegionInfo, y []VPCRegionInfo) bool {
	eqCtr := 0
	for _, a := range x {
		for _, b := range y {
			if reflect.DeepEqual(a, b) {
				eqCtr++
			}
		}
	}
	if eqCtr != len(x) || len(y) != len(x) {
		return false
	}
	return true
}
