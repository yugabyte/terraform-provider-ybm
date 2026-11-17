/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package managed

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *dataVPCType) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The data source to fetch VPC in YugabyteDB Aeon by VPC name or ID.`,
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
				Computed:    true,
				Optional:    true,
			},
			"cloud": schema.StringAttribute{
				Description: "The cloud provider (AWS, AZURE or GCP) where the VPC is to be created.",
				Computed:    true,
			},
			"global_cidr": schema.StringAttribute{
				Description: "The global CIDR of the VPC (GCP only).",
				Computed:    true,
			},
			"region_cidr_info": schema.ListNestedAttribute{
				Description: "The CIDR information for all the regions for the VPC.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"region": schema.StringAttribute{
							Optional: true,
						},
						"cidr": schema.StringAttribute{
							Optional: true,
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func newDataSourceVPC() datasource.DataSource {
	return &dataVPCType{}
}

func (r *dataVPCType) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc"
}

func (r *dataVPCType) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type dataVPCType struct {
	p provider
}

func (r dataVPCType) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

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
	if (!config.Name.IsUnknown() && !config.Name.IsNull()) || config.Name.ValueString() != "" {
		vpcNamePresent = true
	}

	vpcIDPresent := false
	if (!config.VPCID.IsUnknown() && !config.VPCID.IsNull()) || config.VPCID.ValueString() != "" {
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

	vpc, error := dataSourceVPCRead(ctx, accountId, projectId, config.Name.ValueString(), config.VPCID.ValueString(), apiClient)

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

	vpc.AccountID = types.StringValue(accountId)
	vpc.ProjectID = types.StringValue(projectId)
	vpc.VPCID = types.StringValue(vpcResp.GetInfo().Id)
	vpc.Name = types.StringValue(vpcResp.Spec.GetName())
	vpc.Cloud = types.StringValue(string(vpcResp.Spec.GetCloud()))
	vpc.ExternalVPCID = types.StringValue(vpcResp.Info.GetExternalVpcId())
	if vpcResp.Spec.GetParentCidr() != "" {
		vpc.GlobalCIDR = types.StringValue(vpcResp.Spec.GetParentCidr())
	} else {
		vpc.GlobalCIDR = types.StringNull()
	}

	if v, ok := vpcResp.Spec.GetRegionSpecsOk(); ok {
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
	return vpc, nil
}
