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
						Description: "The cloud(eg. AWS or GCP) on which the application is deployed.",
						Type:        types.StringType,
						Required:    true,
					},
					"project": {
						Description: "The account ID for AWS and project ID for GCP.",
						Type:        types.StringType,
						Required:    true,
					},
					"region": {
						Description: "The region in the cloud where the application is deployed.",
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
	applicationVPCID := plan.ApplicationVPCInfo.VPCID.Value

	applicationVPCSpec := *openapiclient.NewCustomerVpcSpec(*openapiclient.NewVpcCloudInfo(openapiclient.CloudEnum(applicationCloud)), applicationProject, applicationVPCID)
	if applicationCloud == "AWS" {
		if plan.ApplicationVPCInfo.Region.Null {
			resp.Diagnostics.AddError("Invalid Input", "Application VPC region must be provided for AWS.")
			return
		}
		applicationRegion := plan.ApplicationVPCInfo.Region.Value
		applicationVPCSpec.CloudInfo.SetRegion(applicationRegion)
		if plan.ApplicationVPCInfo.CIDR.Null {
			resp.Diagnostics.AddError("Invalid Input", "Application VPC CIDR must be provided for AWS.")
			return
		}
		applicationVPCCIDR := plan.ApplicationVPCInfo.CIDR.Value
		applicationVPCSpec.SetCidr(applicationVPCCIDR)
	}
	vpcPeeringSpec := *openapiclient.NewVpcPeeringSpec(applicationVPCSpec, yugabyteDBVPCID, vpcPeeringName)

	vpcPeeringResp, response, err := apiClient.NetworkApi.CreateVpcPeering(ctx, accountId, projectId).VpcPeeringSpec(vpcPeeringSpec).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(response, true)
		resp.Diagnostics.AddError("Could not create vpc peering", string(b))
		return
	}
	vpcPeeringId := vpcPeeringResp.Data.Info.Id

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		vpcResp, _, err := apiClient.NetworkApi.GetVpcPeering(ctx, accountId, projectId, vpcPeeringId).Execute()
		if err == nil {
			if vpcResp.Data.Info.State == "ACTIVE" || vpcResp.Data.Info.State == "PENDING" {
				return nil
			}
		}
		return retry.RetryableError(errors.New("The vpc peering creation didn't succeed yet"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not create vpc peering", "Timed out waiting for vpc peering creation to be successful.")
		return
	}

	vpcPeering, readOK, message := resourceVPCPeeringRead(accountId, projectId, vpcPeeringId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Could not read the state of the vpc peering", message)
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
		resp.Diagnostics.AddError("Could not read the state of the vpc peering", message)
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

	resp.Diagnostics.AddError("Could not update vpc peering.", "Updating a vpc peering is not supported yet. Please delete and recreate.")
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
		resp.Diagnostics.AddError("Could not delete the vpc peering", string(b))
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
		return retry.RetryableError(errors.New("The vpc peering deletion didn't succeed yet"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Could not delete vpc peering", "Timed out waiting for vpc peering deletion to be successful.")
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import vpc
func (r resourceVPCPeering) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
