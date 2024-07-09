package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type dataSourceAllowListType struct{}

func (r dataSourceAllowListType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The data source to fetch an allow list in YugabyteDB Aeon.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this allow list belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this allow list belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"allow_list_id": {
				Description: "The ID of the allow list. Created automatically when an allow list is created. Use this ID to get a specific allow list.",
				Type:        types.StringType,
				Computed:    true,
			},
			"allow_list_name": {
				Description: "The name of the allow list.",
				Type:        types.StringType,
				Required:    true,
			},
			"allow_list_description": {
				Description: "The description of the allow list.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cidr_list": {
				Description: "The CIDR list of the allow list.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Computed: true,
			},
			"cluster_ids": {
				Description: "List of the IDs of the clusters the allow list is assigned to.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Computed: true,
			},
		},
	}, nil
}

func (r dataSourceAllowListType) NewDataSource(_ context.Context, p tfsdk.Provider) (tfsdk.DataSource, diag.Diagnostics) {
	return dataSourceAllowList{
		p: *(p.(*provider)),
	}, nil
}

type dataSourceAllowList struct {
	p provider
}

func (r dataSourceAllowList) Read(ctx context.Context, req tfsdk.ReadDataSourceRequest, resp *tfsdk.ReadDataSourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var config AllowList
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

	allowList, readOK, message := resourceAllowListRead(accountId, projectId, config.AllowListName.Value, apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to fetch allow list %s", config.AllowListName.Value), message)
		return
	}

	tflog.Debug(ctx, "Allow List Read: Allow list on read from API server", map[string]interface{}{
		"Allow List": allowList})

	diags = resp.State.Set(ctx, &allowList)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
