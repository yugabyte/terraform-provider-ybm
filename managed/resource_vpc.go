/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

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
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this VPC belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"vpc_id": {
				Description: "The ID of the VPC. Created automatically when the VPC is created. Used to get a specific VPC.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"external_vpc_id": {
				Description: "The VPC ID for the YB Managed VPC in the cloud provider(AWS/GCP/Azure)",
				Type:        types.StringType,
				Computed:    true,
			},
			"name": {
				Description: "The description of the VPC.",
				Type:        types.StringType,
				Required:    true,
			},
			"cloud": {
				Description: "The cloud provider (AWS, AZURE or GCP) where the VPC is to be created.",
				Type:        types.StringType,
				Required:    true,
			},
			"global_cidr": {
				Description: "The global CIDR of the VPC (GCP only).",
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

	if (!plan.VPCID.Unknown && !plan.VPCID.Null) || plan.VPCID.Value != "" {
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
			"Global and region CIDR details provided",
			"Specify either the global CIDR or the CIDR information for the regions. Don't provide both.",
		)
		return
	}

	vpcName := plan.Name.Value
	cloud := plan.Cloud.Value

	if cloud != "GCP" && globalCIDRPresent {
		resp.Diagnostics.AddError(
			"Global CIDR specified",
			"Global CIDR only applies to GCP.",
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
				"Duplicate regions",
				"Ensure the regions are unique.",
			)
			return
		}
	}

	vpcSpec := *openapiclient.NewSingleTenantVpcSpec(vpcName, openapiclient.CloudEnum(cloud), vpcRegionSpec)
	if globalCIDRPresent {
		vpcSpec.SetParentCidr(plan.GlobalCIDR.Value)
	}
	vpcRequest := *openapiclient.NewSingleTenantVpcRequest(vpcSpec)

	vpcResp, response, err := apiClient.NetworkApi.CreateVpc(ctx, accountId, projectId).SingleTenantVpcRequest(vpcRequest).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create VPC", errMsg)
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
		return retry.RetryableError(errors.New("VPC creation in progress."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create VPC", "The operation timed out waiting for VPC creation.")
		return
	}

	vpc, readOK, message := resourceVPCRead(accountId, projectId, vpcId, regionMap, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the VPC", message)
		return
	}
	if !globalCIDRPresent {
		vpc.GlobalCIDR.Null = true
	} else {
		vpc.RegionCIDRInfo = nil
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

func resourceVPCRead(accountId string, projectId string, vpcId string, regionMap map[string]int, apiClient *openapiclient.APIClient) (vpc VPC, readOK bool, errorMessage string) {
	vpcResp, response, err := apiClient.NetworkApi.GetSingleTenantVpc(context.Background(), accountId, projectId, vpcId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return vpc, false, errMsg
	}

	vpc.AccountID.Value = accountId
	vpc.ProjectID.Value = projectId
	vpc.VPCID.Value = vpcId

	vpc.Name.Value = vpcResp.Data.Spec.GetName()
	vpc.Cloud.Value = string(vpcResp.Data.Spec.GetCloud())
	vpc.ExternalVPCID.Value = vpcResp.Data.Info.GetExternalVpcId()
	if vpcResp.Data.Spec.GetParentCidr() != "" {
		vpc.GlobalCIDR.Value = vpcResp.Data.Spec.GetParentCidr()
	} else {
		vpc.GlobalCIDR.Null = true
	}

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
		resp.Diagnostics.AddError("Unable to read the state of the VPC", message)
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

	resp.Diagnostics.AddError("Unable to update VPC.", "Updating VPCs is not currently supported. Delete and recreate the provider.")
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
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to delete the VPC", errMsg)
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
		return retry.RetryableError(errors.New("VPC deletion in progress."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to delete VPC", "The operation timed out waiting for the VPC to be deleted.")
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import vpc
func (r resourceVPC) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
