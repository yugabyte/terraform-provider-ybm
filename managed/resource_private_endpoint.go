/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	"github.com/yugabyte/terraform-provider-ybm/managed/util"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourcePrivateEndpointType struct{}

func (r resourcePrivateEndpointType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create an endpoint in YugabyteDB Aeon.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this private service endpoint belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this private service endpoint belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_id": {
				Description: "The ID of the cluster.",
				Type:        types.StringType,
				Required:    true,
			},
			"endpoint_id": {
				Description: "The ID of the endpoint. Created automatically when the private endpoint is created. Used to get a specific Endpoint.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"region": {
				Description: "Region where the service endpoint should be created",
				Type:        types.StringType,
				Required:    true,
			},
			"security_principals": {
				Description: "List of security principals that have access to this endpoint. Required for private service endpoints. ",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Required: true,
			},
			"state": {
				Description: "State of the private service endpoint",
				Type:        types.StringType,
				Computed:    true,
			},
			"host": {
				Description: "Endpoint to connect to the cluster",
				Type:        types.StringType,
				Computed:    true,
			},
			"availability_zones": {
				Description: "Availability Zone of the endpoint.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Computed: true,
			},
			"service_name": {
				Description: "Name of the Service endpoint",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_region_info_id": {
				Description: "Cluster region ID. This field is computed ",
				Type:        types.StringType,
				Computed:    true,
			},
		},
	}, nil
}
func (r resourcePrivateEndpointType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourcePrivateEndpoint{
		p: *(p.(*provider)),
	}, nil
}

type resourcePrivateEndpoint struct {
	p provider
}

func getIDsFromePrivateEndpointState(ctx context.Context, state tfsdk.State, pse *PrivateServiceEndpoint) {
	state.GetAttribute(ctx, path.Root("account_id"), &pse.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &pse.ProjectID)
	state.GetAttribute(ctx, path.Root("endpoint_id"), &pse.PrivateServiceEndpointID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &pse.ClusterID)
	state.GetAttribute(ctx, path.Root("cluster_region_info_id"), &pse.ClusterRegionInfoId)
}

func getPrivateEndpointServicePlan(ctx context.Context, plan tfsdk.Plan, pse *PrivateServiceEndpoint) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &PrivateServiceEndpoint)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &pse.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("region"), &pse.Region)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("security_principals"), &pse.SecurityPrincipals)...)

	return diags
}

// Read Private service endpoint
func (r resourcePrivateEndpoint) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state PrivateServiceEndpoint
	getIDsFromePrivateEndpointState(ctx, req.State, &state)
	pseID := state.PrivateServiceEndpointID.Value
	clusterID := state.ClusterID.Value

	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	pse, readOK, message := resourcePrivateEndpointRead(accountId, projectId, pseID, clusterID, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the private service endpoint ", message)
		return
	}

	diags := resp.State.Set(ctx, &pse)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourcePrivateEndpoint) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan PrivateServiceEndpoint
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getPrivateEndpointServicePlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the private service endpoint")
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	if plan.PrivateServiceEndpointID.Value != "" {
		resp.Diagnostics.AddError(
			"Private service endpoint ID provided for new private service endpoint",
			"The endpoint_id was provided even though a new private service endpoint is being created. Do not include this field in the provider when creating it.",
		)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}
	clusterId := plan.ClusterID.Value
	region := plan.Region.Value
	securityPrincipals := plan.SecurityPrincipals

	clusterResp, _, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to fetch cluster with id %s", clusterId), GetApiErrorDetails(err))
		return
	}
	clusterData := clusterResp.GetData()

	if string(clusterData.Spec.CloudInfo.Code) == "GCP" {
		resp.Diagnostics.AddError(
			"Private service endpoint is only supported for AWS or AZURE clusters.",
			"Please make sure your cluster is on AWS or AZURE.",
		)
		return
	}

	allClusterRegions := clusterData.Info.ClusterRegionInfoDetails
	desiredRegions := util.Filter(allClusterRegions, func(regionInfo openapiclient.ClusterRegionInfoDetails) bool {
		return regionInfo.Region == region
	})

	if len(desiredRegions) == 0 {
		resp.Diagnostics.AddError(fmt.Sprintf("No region found for cluster %s with region %s\n", clusterId, region), "")
		return
	}
	if len(desiredRegions) > 1 {
		resp.Diagnostics.AddError(fmt.Sprintf("Multiple regions found for cluster %s with region %s\n", clusterId, region), "")
		return
	}

	regionArnMap := make(map[string][]string)
	regionArnMap[desiredRegions[0].Id] = util.SliceTypesStringToSliceString(securityPrincipals)
	createPseSpec := createPrivateServiceEndpointSpec(regionArnMap)

	createResp, _, err := apiClient.ClusterApi.CreatePrivateServiceEndpoint(context.Background(), accountId, projectId, clusterId).PrivateServiceEndpointSpec(createPseSpec[0]).Execute()
	if err != nil {
		resp.Diagnostics.AddError("unable to create private service endpoint", GetApiErrorDetails(err))
		return
	}
	psEps := util.Filter(createResp.GetData(), func(ep openapiclient.PrivateServiceEndpointRegionData) bool {
		return *ep.GetSpec().ClusterRegionInfoId.Get() == desiredRegions[0].Id
	})

	if len(psEps) == 0 {
		resp.Diagnostics.AddError("unable to create private service endpoint",
			"Could not find cluster region endpoint")
		return
	}

	pseID := psEps[0].Info.Id

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		pseState, readInfoOK, message := getPrivateServiceEndpointStateFromCluster(accountId, projectId, clusterId, pseID, apiClient)
		if readInfoOK {
			if pseState == string(openapiclient.ENDPOINTSTATEENUM_ENABLED) {
				return nil
			}
			if pseState == string(openapiclient.ENDPOINTSTATEENUM_FAILED) {
				return errors.New("unable to create private service endpoint, operation failed")
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get private service endpoint state: " + message))
		}
		return retry.RetryableError(errors.New("the private service endpoint creation is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create private service endpoint ", "The operation timed out waiting for private service endpoint creation.")
		return
	}

	pse, readOK, message := resourcePrivateEndpointRead(accountId, projectId, pseID, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the private service endpoint ", message)
		return
	}

	diags := resp.State.Set(ctx, &pse)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func resourcePrivateEndpointRead(accountId string, projectId string, pseID string, clusterId string, apiClient *openapiclient.APIClient) (pse PrivateServiceEndpoint, readOK bool, errorMessage string) {
	pseResp, _, err := apiClient.ClusterApi.GetPrivateServiceEndpoint(context.Background(), accountId, projectId, clusterId, pseID).Execute()
	if err != nil {
		return pse, false, GetApiErrorDetails(err)
	}
	pse.AccountID.Value = accountId
	pse.ProjectID.Value = projectId
	pse.PrivateServiceEndpointID.Value = pseResp.Data.Info.Id
	pse.ClusterID.Value = clusterId
	pse.ServiceName.Value = pseResp.Data.Info.GetServiceName()

	pse.AvailabilityZones = util.SliceStringToSliceTypesString(pseResp.Data.Info.AvailabilityZones)
	pse.SecurityPrincipals = util.SliceStringToSliceTypesString(pseResp.Data.Spec.Get().SecurityPrincipals)

	pse.ClusterRegionInfoId.Value = pseResp.Data.Spec.Get().GetClusterRegionInfoId()

	// This another call, but we should get all information as much as possible from the API
	// This is usefull when importing
	clusterResp, _, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		return pse, false, fmt.Sprintf("Unable to fetch cluster with id %s: %s", clusterId, GetApiErrorDetails(err))
	}
	clusterData := clusterResp.GetData()
	pse.ClusterRegionInfoId.Value = pseResp.Data.Spec.Get().GetClusterRegionInfoId()
	desiredRegions := util.Filter(clusterData.Info.ClusterRegionInfoDetails, func(regionInfo openapiclient.ClusterRegionInfoDetails) bool {
		return regionInfo.Id == pse.ClusterRegionInfoId.Value
	})
	desiredEndpoints := util.Filter(clusterData.Info.ClusterEndpoints, func(endpoint openapiclient.Endpoint) bool {
		if !endpoint.HasPseId() {
			return false
		}
		return endpoint.GetPseId() == pse.PrivateServiceEndpointID.Value
	})
	if len(desiredEndpoints) == 1 {
		pse.Host.Value = desiredEndpoints[0].GetHost()
		pse.State.Value = string(desiredEndpoints[0].GetStateV1())
	}
	pse.Region.Value = desiredRegions[0].Region

	return pse, true, ""

}

// Update private service Endpoint
func (r resourcePrivateEndpoint) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan PrivateServiceEndpoint
	resp.Diagnostics.Append(getPrivateEndpointServicePlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the private service endpoint")
		return
	}

	apiClient := r.p.client
	var state PrivateServiceEndpoint
	getIDsFromePrivateEndpointState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	ClusterRegionInfoId := state.ClusterRegionInfoId.Value
	pseId := state.PrivateServiceEndpointID.Value

	//Security Principal is the only field we update
	securityPrincipals := plan.SecurityPrincipals

	regionArnMap := make(map[string][]string)
	regionArnMap[ClusterRegionInfoId] = util.SliceTypesStringToSliceString(securityPrincipals)
	createPseSpec := createPrivateServiceEndpointRegionSpec(regionArnMap)

	_, _, err := apiClient.ClusterApi.EditPrivateServiceEndpoint(context.Background(), accountId, projectId, clusterId, pseId).PrivateServiceEndpointRegionSpec(createPseSpec[0]).Execute()
	if err != nil {
		resp.Diagnostics.AddError("unable to update private service endpoint", GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		pseState, readInfoOK, message := getPrivateServiceEndpointStateFromCluster(accountId, projectId, clusterId, pseId, apiClient)
		if readInfoOK {
			if pseState == string(openapiclient.ENDPOINTSTATEENUM_ENABLED) {
				return nil
			}
			if pseState == string(openapiclient.ENDPOINTSTATEENUM_FAILED) {
				return errors.New("unable to update private service endpoint, operation failed")
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get private service endpoint state: " + message))
		}
		return retry.RetryableError(errors.New("the private service endpoint update is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to update private service endpoint ", "The operation timed out waiting for private service endpoint creation.")
		return
	}

	pse, readOK, message := resourcePrivateEndpointRead(accountId, projectId, pseId, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the private service endpoint ", message)
		return
	}

	// Temporaly fix
	// Backend return the old service principal as there seems to be an async op
	// Instead of waiting, we just assign the service principal
	time.Sleep(20 * time.Second)
	pse.SecurityPrincipals = plan.SecurityPrincipals
	//end fix

	diags := resp.State.Set(ctx, &pse)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

// Delete allow list
func (r resourcePrivateEndpoint) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state PrivateServiceEndpoint
	getIDsFromePrivateEndpointState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	pseId := state.PrivateServiceEndpointID.Value
	clusterID := state.ClusterID.Value

	apiClient := r.p.client

	_, err := apiClient.ClusterApi.DeletePrivateServiceEndpoint(context.Background(), accountId, projectId, clusterID, pseId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete the private Service endpoint ", GetApiErrorDetails(err))
		return
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		pseState, readInfoOK, message := getPrivateServiceEndpointState(accountId, projectId, clusterID, pseId, apiClient)
		if readInfoOK {
			if pseState == string(openapiclient.PRIVATESERVICEENDPOINTSTATEENUM_DELETED) {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get private service endpoint state: " + message))
		}
		return retry.RetryableError(errors.New("the private service endpoint deletion is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to delete private service endpoint ", "The operation timed out waiting for private service endpoint deletion.")
		return
	}

	resp.State.RemoveResource(ctx)

}

func createPrivateServiceEndpointSpec(regionArnMap map[string][]string) []openapiclient.PrivateServiceEndpointSpec {
	pseSpecs := []openapiclient.PrivateServiceEndpointSpec{}

	for regionId, arnList := range regionArnMap {
		regionSpec := *openapiclient.NewPrivateServiceEndpointRegionSpec(arnList)
		regionSpec.SetClusterRegionInfoId(regionId)
		local := *openapiclient.NewPrivateServiceEndpointSpec([]openapiclient.PrivateServiceEndpointRegionSpec{regionSpec})
		pseSpecs = append(pseSpecs, local)
	}
	return pseSpecs
}

func createPrivateServiceEndpointRegionSpec(regionArnMap map[string][]string) []openapiclient.PrivateServiceEndpointRegionSpec {
	pseSpecs := []openapiclient.PrivateServiceEndpointRegionSpec{}

	for regionId, arnList := range regionArnMap {
		local := *openapiclient.NewPrivateServiceEndpointRegionSpec(arnList)
		local.SetClusterRegionInfoId(regionId)
		pseSpecs = append(pseSpecs, local)
	}
	return pseSpecs
}

// Import PSE
func (r resourcePrivateEndpoint) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	idParts := strings.Split(req.ID, ",")

	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: endpoint_id,cluster_id. Got: %q", req.ID),
		)
		return
	}
	// Save the import identifier in the endpoint_id & cluster_id attribute
	resp.State.SetAttribute(ctx, path.Root("endpoint_id"), idParts[0])
	resp.State.SetAttribute(ctx, path.Root("cluster_id"), idParts[1])
}

func getPrivateServiceEndpointState(accountId string, projectId string, clusterId string, pseID string, apiClient *openapiclient.APIClient) (state string, readOK bool, errorMessage string) {
	pseResp, resp, err := apiClient.ClusterApi.GetPrivateServiceEndpoint(context.Background(), accountId, projectId, clusterId, pseID).Execute()
	if err != nil {
		//Most likely is deleted
		if resp.StatusCode == 404 {
			return string(openapiclient.PRIVATESERVICEENDPOINTSTATEENUM_DELETED), true, ""
		}
		return "", false, GetApiErrorDetails(err)
	}
	return string(pseResp.Data.Info.GetState()), true, ""
}

func getPrivateServiceEndpointStateFromCluster(accountId string, projectId string, clusterId string, pseId string, apiClient *openapiclient.APIClient) (state string, readOK bool, errorMessage string) {
	clusterResp, _, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		return "", false, GetApiErrorDetails(err)
	}
	clusterData := clusterResp.GetData()
	desiredEndpoints := util.Filter(clusterData.Info.ClusterEndpoints, func(endpoint openapiclient.Endpoint) bool {
		if !endpoint.HasPseId() {
			return false
		}
		return endpoint.GetPseId() == pseId
	})
	if len(desiredEndpoints) != 1 {
		return "", false, "Could not find endpoint id from Cluster API"
	}
	return string(desiredEndpoints[0].GetStateV1()), true, ""

}
