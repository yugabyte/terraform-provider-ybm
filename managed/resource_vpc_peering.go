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
		Description: `The resource to create a VPC peering in YugabyteDB Managed.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this VPC peering belongs to. To be provided if there are multiple accounts associated with the user.",
				Type:        types.StringType,
				Optional:    true,
			},
			"project_id": {
				Description: "The ID of the project this VPC peering belongs to.",
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
				Description: "The details for the VPC where the application is deployed.",
				Required:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"cloud": {
						Description: "The cloud provider (AWS or GCP) where the application is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
					"project": {
						Description: "The account ID (AWS) or project ID (GCP).",
						Type:        types.StringType,
						Required:    true,
					},
					"region": {
						Description: "The region where the application is deployed.",
						Type:        types.StringType,
						Optional:    true,
					},
					"vpc_id": {
						Description: "The ID of the VPC in which the application is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
					"cidr": {
						Description: "The CIDR of the VPC in which the application is deployed.",
						Type:        types.StringType,
						Optional:    true,
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
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan VPCPeering
	var accountId, message string
	var getAccountOK bool
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(getVPCPeeringPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client
	if !plan.AccountID.Null {
		accountId = plan.AccountID.Value
	} else {
		accountId, getAccountOK, message = getAccountId(apiClient)
		if !getAccountOK {
			resp.Diagnostics.AddError("Unable to get account ID", message)
			return
		}
	}

	if (!plan.VPCPeeringID.Unknown && !plan.VPCPeeringID.Null) || plan.VPCPeeringID.Value != "" {
		resp.Diagnostics.AddError(
			"VPC Peering ID provided for new VPC peering",
			"The vpc_peering_id was provided even though a new VPC peering is being created. Do not include this field in the provider when creating a new peering.",
		)
		return
	}
	projectId, getProjectOK, message := getProjectId(accountId, apiClient)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}

	vpcPeeringName := plan.Name.Value
	yugabyteDBVPCID := plan.YugabyteDBVPCID.Value
	applicationCloud := plan.ApplicationVPCInfo.Cloud.Value
	applicationProject := plan.ApplicationVPCInfo.Project.Value
	applicationVPCID := plan.ApplicationVPCInfo.VPCID.Value

	applicationVPCSpec := *openapiclient.NewCustomerVpcSpec(*openapiclient.NewVpcCloudInfo(openapiclient.CloudEnum(applicationCloud)), applicationProject, applicationVPCID)

	// The Region and CIDR are required only for AWS. They are not required for GCP.
	if applicationCloud == "AWS" {
		if plan.ApplicationVPCInfo.Region.Null {
			resp.Diagnostics.AddError("No region specified", "You must specify the application VPC region for AWS.")
			return
		}
		applicationRegion := plan.ApplicationVPCInfo.Region.Value
		applicationVPCSpec.CloudInfo.SetRegion(applicationRegion)
		if plan.ApplicationVPCInfo.CIDR.Null {
			resp.Diagnostics.AddError("No CIDR specified", "You must specify the CIDR of the application VPC for AWS.")
			return
		}
		applicationVPCCIDR := plan.ApplicationVPCInfo.CIDR.Value
		applicationVPCSpec.SetCidr(applicationVPCCIDR)
	}
	vpcPeeringSpec := *openapiclient.NewVpcPeeringSpec(applicationVPCSpec, yugabyteDBVPCID, vpcPeeringName)

	vpcPeeringResp, response, err := apiClient.NetworkApi.CreateVpcPeering(ctx, accountId, projectId).VpcPeeringSpec(vpcPeeringSpec).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Unable to create VPC peering", string(b))
		return
	}
	vpcPeeringId := vpcPeeringResp.Data.Info.Id

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		vpcResp, _, err := apiClient.NetworkApi.GetVpcPeering(ctx, accountId, projectId, vpcPeeringId).Execute()
		if err == nil {
			// VPC peering is a 2 step process. Once it is in pending state, it is up to the customer to confirm the peering.
			if vpcResp.Data.Info.State == "ACTIVE" || vpcResp.Data.Info.State == "PENDING" {
				return nil
			}
		}
		return retry.RetryableError(errors.New("VPC peering creation in progress."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not create vpc peering", "The operation timed out waiting for the VPC peering creation.")
		return
	}

	vpcPeering, readOK, message := resourceVPCPeeringRead(accountId, projectId, vpcPeeringId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the VPC peering", message)
		return
	}

	if plan.ApplicationVPCInfo.Region.Null {
		vpcPeering.ApplicationVPCInfo.Region.Null = true
	}
	if plan.ApplicationVPCInfo.CIDR.Null {
		vpcPeering.ApplicationVPCInfo.CIDR.Null = true
	}

	diags = resp.State.Set(ctx, &vpcPeering)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromVPCPeeringState(ctx context.Context, state tfsdk.State, vpcPeering *VPCPeering) {
	state.GetAttribute(ctx, path.Root("account_id"), &vpcPeering.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &vpcPeering.ProjectID)
	state.GetAttribute(ctx, path.Root("vpc_peering_id"), &vpcPeering.VPCPeeringID)
}

func resourceVPCPeeringRead(accountId string, projectId string, vpcPeeringId string, apiClient *openapiclient.APIClient) (vpcPeering VPCPeering, readOK bool, errorMessage string) {
	vpcPeeringResp, response, err := apiClient.NetworkApi.GetVpcPeering(context.Background(), accountId, projectId, vpcPeeringId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		return vpcPeering, false, string(b)
	}

	vpcPeering.AccountID.Value = accountId
	vpcPeering.ProjectID.Value = projectId
	vpcPeering.VPCPeeringID.Value = vpcPeeringId

	vpcPeering.Name.Value = vpcPeeringResp.Data.Spec.GetName()
	vpcPeering.YugabyteDBVPCID.Value = vpcPeeringResp.Data.Spec.GetInternalYugabyteVpcId()
	vpcPeering.VPCPeeringState.Value = string(vpcPeeringResp.Data.Info.GetState())
	vpcPeering.ApplicationVPCInfo.Cloud.Value = string(vpcPeeringResp.Data.Spec.CustomerVpc.CloudInfo.GetCode())
	vpcPeering.ApplicationVPCInfo.Project.Value = vpcPeeringResp.Data.Spec.CustomerVpc.GetCloudProviderProject()
	vpcPeering.ApplicationVPCInfo.Region.Value = vpcPeeringResp.Data.Spec.CustomerVpc.CloudInfo.GetRegion()
	vpcPeering.ApplicationVPCInfo.VPCID.Value = vpcPeeringResp.Data.Spec.CustomerVpc.GetExternalVpcId()
	vpcPeering.ApplicationVPCInfo.CIDR.Value = vpcPeeringResp.Data.Spec.CustomerVpc.GetCidr()

	return vpcPeering, true, ""
}

// Read vpc peering
func (r resourceVPCPeering) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state VPCPeering
	getIDsFromVPCPeeringState(ctx, req.State, &state)

	vpc, readOK, message := resourceVPCPeeringRead(state.AccountID.Value, state.ProjectID.Value, state.VPCPeeringID.Value, r.p.client)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the VPC peering", message)
		return
	}

	diags := resp.State.Set(ctx, &vpc)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update vpc peering
func (r resourceVPCPeering) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {

	resp.Diagnostics.AddError("Unable to update VPC peering.", "Updating a VPC peering is not currently supported. Delete and recreate the provider.")
	return

}

// Delete vpc peering
func (r resourceVPCPeering) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state VPCPeering
	getIDsFromVPCPeeringState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	vpcPeeringId := state.VPCPeeringID.Value

	apiClient := r.p.client

	response, err := apiClient.NetworkApi.DeleteVpcPeering(ctx, accountId, projectId, vpcPeeringId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Unable to delete the VPC peering", string(b))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(300*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		_, resp, err := apiClient.NetworkApi.GetVpcPeering(ctx, accountId, projectId, vpcPeeringId).Execute()
		if err != nil {
			if resp.StatusCode == 404 {
				return nil
			}
		}
		return retry.RetryableError(errors.New("VPC peering deletion in progress."))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to delete VPC peering", "The operation timed out waiting for the VPC peering to be deleted.")
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import vpc peering
func (r resourceVPCPeering) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
