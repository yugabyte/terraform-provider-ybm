/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package managed

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type dataSourceVPCType struct{}

func (r dataSourceVPCType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The data source to fetch VPC in YugabyteDB Managed by VPC name or ID.`,
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
				Description: "The ID of the cloud provider(AWS/GCP/Azure) VPC where YBM resources are created",
				Type:        types.StringType,
				Computed:    true,
			},
			"name": {
				Description: "The description of the VPC.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"cloud": {
				Description: "The cloud provider (AWS, AZURE or GCP) where the VPC is to be created.",
				Type:        types.StringType,
				Computed:    true,
			},
			"global_cidr": {
				Description: "The global CIDR of the VPC (GCP only).",
				Type:        types.StringType,
				Computed:    true,
			},
			"region_cidr_info": {
				Description: "The CIDR information for all the regions for the VPC.",
				Computed:    true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"region": {
						Type:     types.StringType,
						Optional: true,
					},
					"cidr": {
						Type:     types.StringType,
						Optional: true,
						Computed: true,
					},
				}),
			},
		},
	}, nil
}

func (r dataSourceVPCType) NewDataSource(_ context.Context, p tfsdk.Provider) (tfsdk.DataSource, diag.Diagnostics) {
	return dataVPCType{
		p: *(p.(*provider)),
	}, nil
}

type dataVPCType struct {
	p provider
}

func (r dataVPCType) Read(ctx context.Context, req tfsdk.ReadDataSourceRequest, resp *tfsdk.ReadDataSourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var config VPC
	var accountId, message string
	var getAccountOK bool
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
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
		resp.Diagnostics.AddError("Unable to get the project ID ", message)
		return
	}

	vpcNamePresent := false
	if (!config.Name.Unknown && !config.Name.Null) || config.Name.Value != "" {
		vpcNamePresent = true
	}

	vpcIDPresent := false
	if (!config.VPCID.Unknown && !config.VPCID.Null) || config.VPCID.Value != "" {
		vpcIDPresent = true
	}

	// Exactly one parameter amongst name and vpc_id must be present
	// Simulating XOR by comparing boolean values
	if vpcNamePresent == vpcIDPresent {
		resp.Diagnostics.AddError(
			"Specify VPC name or VPC ID",
			"To select a vpc, use either name or vpc_id. Don't provide both.",
		)
		return
	}

	vpc, error := dataSourceVPCRead(ctx, accountId, projectId, config.Name.Value, config.VPCID.Value, apiClient)

	if error != nil {
		resp.Diagnostics.AddError(error.Error(), "")
		return
	}

	diags = resp.State.Set(ctx, &vpc)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getVPCByName(ctx context.Context, accountId string, projectId string, vpcName string, apiClient *openapiclient.APIClient) (vpcData openapiclient.SingleTenantVpcDataResponse, err error) {

	resq := apiClient.NetworkApi.ListSingleTenantVpcs(ctx, accountId, projectId).Name(vpcName)
	response, _, err := resq.Execute()
	if err != nil {
		return vpcData, fmt.Errorf("unable to read the state of the vpc %s", GetApiErrorDetails(err))
	}
	if len(response.GetData()) < 1 {
		return vpcData, fmt.Errorf("VPC %s not found", vpcName)

	}

	if len(response.GetData()) > 1 {
		return vpcData, fmt.Errorf("more than 1 VPC found")

	}

	return response.GetData()[0], nil
}

func getVPCByID(ctx context.Context, accountId string, projectId string, vpcId string, apiClient *openapiclient.APIClient) (vpcData openapiclient.SingleTenantVpcDataResponse, err error) {
	resq := apiClient.NetworkApi.GetSingleTenantVpc(ctx, accountId, projectId, vpcId)
	response, _, err := resq.Execute()
	if err != nil {
		return vpcData, fmt.Errorf("unable to read the state of the vpc %s", GetApiErrorDetails(err))
	}
	if v, ok := response.GetDataOk(); ok && v != nil {
		return *v, nil
	}

	return vpcData, fmt.Errorf("VPC %s not found", vpcId)
}

func dataSourceVPCRead(ctx context.Context, accountId string, projectId string, vpcName string, vpcId string, apiClient *openapiclient.APIClient) (vpc VPC, err error) {
	var vpcResp openapiclient.SingleTenantVpcDataResponse

	if len(vpcName) > 0 {
		vpcResp, err = getVPCByName(ctx, accountId, projectId, vpcName, apiClient)
	} else {
		vpcResp, err = getVPCByID(ctx, accountId, projectId, vpcId, apiClient)
	}
	if err != nil {
		return vpc, err
	}

	vpc.AccountID.Value = accountId
	vpc.ProjectID.Value = projectId
	vpc.VPCID.Value = vpcResp.GetInfo().Id
	vpc.Name.Value = vpcResp.Spec.GetName()
	vpc.Cloud.Value = string(vpcResp.Spec.GetCloud())
	vpc.ExternalVPCID.Value = vpcResp.Info.GetExternalVpcId()
	if vpcResp.Spec.GetParentCidr() != "" {
		vpc.GlobalCIDR.Value = vpcResp.Spec.GetParentCidr()
	} else {
		vpc.GlobalCIDR.Null = true
	}

	if v, ok := vpcResp.Spec.GetRegionSpecsOk(); ok {
		regionInfo := make([]VPCRegionInfo, len(*v))
		for index, info := range *v {
			region := info.GetRegion()
			cidr := info.GetCidr()
			regionInfo[index] = VPCRegionInfo{
				Region: types.String{Value: region},
				CIDR:   types.String{Value: cidr},
			}
		}
		sort.Slice(regionInfo, func(i, j int) bool {
			return string(regionInfo[i].Region.Value) < string(regionInfo[j].Region.Value)
		})
		vpc.RegionCIDRInfo = regionInfo
	}
	return vpc, nil
}
