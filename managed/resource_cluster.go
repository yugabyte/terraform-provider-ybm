/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"time"

	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/schemavalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/sethvargo/go-retry"
	"github.com/yugabyte/terraform-provider-ybm/managed/fflags"
	"github.com/yugabyte/terraform-provider-ybm/managed/util"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceClusterType struct{}

// Short function for Base64 encoding
func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func (r resourceClusterType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	attributes := map[string]tfsdk.Attribute{
		"account_id": {
			Description: "The ID of the account this cluster belongs to.",
			Type:        types.StringType,
			Computed:    true,
			PlanModifiers: []tfsdk.AttributePlanModifier{
				tfsdk.UseStateForUnknown(),
			},
		},
		"project_id": {
			Description: "The ID of the project this cluster belongs to.",
			Type:        types.StringType,
			Computed:    true,
			PlanModifiers: []tfsdk.AttributePlanModifier{
				tfsdk.UseStateForUnknown(),
			},
		},
		"cluster_id": {
			Description: "The ID of the cluster. Created automatically when a cluster is created. Used to get a specific cluster.",
			Type:        types.StringType,
			Computed:    true,
			PlanModifiers: []tfsdk.AttributePlanModifier{
				tfsdk.UseStateForUnknown(),
			},
		},
		"cluster_name": {
			Description: "The name of the cluster.",
			Type:        types.StringType,
			Required:    true,
		},
		"cluster_type": {
			Description: "The type of the cluster. SYNCHRONOUS or GEO_PARTITIONED",
			Type:        types.StringType,
			Required:    true,
			Validators: []tfsdk.AttributeValidator{
				stringvalidator.OneOf("SYNCHRONOUS", "GEO_PARTITIONED"),
			},
		},
		"cloud_type": {
			Description: "The cloud provider where the cluster is deployed: AWS, AZURE or GCP.",
			Type:        types.StringType,
			Optional:    true,
			Computed:    true,
			PlanModifiers: []tfsdk.AttributePlanModifier{
				tfsdk.UseStateForUnknown(),
			},
			Validators: []tfsdk.AttributeValidator{stringvalidator.OneOf("AWS", "GCP", "AZURE")},
		},
		"cluster_region_info": {
			Required: true,
			Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
				"num_nodes": {
					Type:     types.Int64Type,
					Required: true,
				},
				"region": {
					Type:     types.StringType,
					Required: true,
				},
				"num_cores": {
					Description: "Number of CPU cores in the nodes of the region.",
					Type:        types.Int64Type,
					Optional:    true,
					Computed:    true,
					Validators: []tfsdk.AttributeValidator{
						schemavalidator.ExactlyOneOf(
							path.MatchRelative().AtParent().AtParent().AtParent().AtName("node_config").AtName("num_cores"),
							path.MatchRelative(),
						),
					},
				},
				"disk_size_gb": {
					Description: "Disk size of the nodes of the region.",
					Type:        types.Int64Type,
					Optional:    true,
					Computed:    true,
					Validators: []tfsdk.AttributeValidator{
						schemavalidator.ConflictsWith(
							path.MatchRelative().AtParent().AtParent().AtParent().AtName("node_config").AtName("disk_size_gb"),
						),
					},
				},
				"disk_iops": {
					Description: "Disk IOPS of the nodes of the region.",
					Type:        types.Int64Type,
					Optional:    true,
					Computed:    true,
					Validators: []tfsdk.AttributeValidator{
						schemavalidator.ConflictsWith(
							path.MatchRelative().AtParent().AtParent().AtParent().AtName("node_config").AtName("disk_iops"),
						),
					},
				},
				"vpc_id": {
					Type:     types.StringType,
					Optional: true,
					Computed: true,
					Validators: []tfsdk.AttributeValidator{
						schemavalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("vpc_name")),
					},
				},
				"vpc_name": {
					Type:     types.StringType,
					Optional: true,
					Computed: true,
				},
				"public_access": {
					Type:     types.BoolType,
					Optional: true,
					Computed: true,
					PlanModifiers: []tfsdk.AttributePlanModifier{
						tfsdk.UseStateForUnknown(),
					},
				},
				"is_preferred": {
					Type:     types.BoolType,
					Optional: true,
					Computed: true,
					PlanModifiers: []tfsdk.AttributePlanModifier{
						tfsdk.UseStateForUnknown(),
					},
				},
				"is_default": {
					Type:     types.BoolType,
					Optional: true,
					Computed: true,
					PlanModifiers: []tfsdk.AttributePlanModifier{
						tfsdk.UseStateForUnknown(),
					},
				},
			}),
		},
		"backup_schedules": {
			Optional: true,
			Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
				"state": {
					Description: "The state of the backup schedule. Used to pause or resume the backup schedule. Valid values are ACTIVE or PAUSED.",
					Type:        types.StringType,
					Computed:    true,
					Optional:    true,
					PlanModifiers: []tfsdk.AttributePlanModifier{
						tfsdk.UseStateForUnknown(),
					},
				},

				"cron_expression": {
					Description: "The cron expression for the backup schedule",
					Type:        types.StringType,
					Computed:    true,
					Optional:    true,
				},

				"time_interval_in_days": {
					Description: "The time interval in days for the backup schedule.",
					Type:        types.Int64Type,
					Computed:    true,
					Optional:    true,
				},

				"retention_period_in_days": {
					Description: "The retention period of the backup schedule.",
					Type:        types.Int64Type,
					Computed:    true,
					Optional:    true,
					PlanModifiers: []tfsdk.AttributePlanModifier{
						tfsdk.UseStateForUnknown(),
					},
				},

				"backup_description": {
					Description: "The description of the backup schedule.",
					Type:        types.StringType,
					Computed:    true,
					Optional:    true,
					PlanModifiers: []tfsdk.AttributePlanModifier{
						tfsdk.UseStateForUnknown(),
					},
				},

				"schedule_id": {
					Description: "The ID of the backup schedule. Created automatically when the backup schedule is created. Used to get a specific backup schedule.",
					Type:        types.StringType,
					Computed:    true,
					Optional:    true,
					PlanModifiers: []tfsdk.AttributePlanModifier{
						tfsdk.UseStateForUnknown(),
					},
				},
				"incremental_interval_in_mins": {
					Description: "The time interval in minutes for the incremental backup schedule.",
					Type:        types.Int64Type,
					Optional:    true,
					Validators:  []tfsdk.AttributeValidator{int64validator.AtLeast(60)},
				},
			}),
		},
		"cmk_spec": {
			Description: "KMS Provider Configuration.",
			Optional:    true,
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"provider_type": {
					Description: "CMK Provider Type.",
					Type:        types.StringType,
					Required:    true,
					Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("AWS", "GCP", "AZURE")},
				},
				"is_enabled": {
					Description: "Is Enabled",
					Type:        types.BoolType,
					Required:    true,
				},
				"aws_cmk_spec": {
					Description: "AWS CMK Provider Configuration.",
					Optional:    true,
					Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
						"access_key": {
							Description: "Access Key",
							Type:        types.StringType,
							Required:    true,
						},
						"secret_key": {
							Description: "Secret Key",
							Type:        types.StringType,
							Required:    true,
						},
						"arn_list": {
							Description: "AWS ARN List",
							Type:        types.ListType{ElemType: types.StringType},
							Required:    true,
						},
					}),
				},
				"gcp_cmk_spec": {
					Description: "GCP CMK Provider Configuration.",
					Optional:    true,
					Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
						"key_ring_name": {
							Description: "Key Ring Name",
							Type:        types.StringType,
							Required:    true,
						},
						"key_name": {
							Description: "Key Name",
							Type:        types.StringType,
							Required:    true,
						},
						"location": {
							Description: "Location",
							Type:        types.StringType,
							Required:    true,
						},
						"protection_level": {
							Description: "Key Protection Level",
							Type:        types.StringType,
							Required:    true,
						},
						"gcp_service_account": {
							Description: "GCP Service Account",
							Required:    true,
							Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
								"type": {
									Description: "Service Account Type",
									Type:        types.StringType,
									Required:    true,
								},
								"project_id": {
									Description: "GCP Project ID",
									Type:        types.StringType,
									Required:    true,
								},
								"private_key": {
									Description: "Private Key",
									Type:        types.StringType,
									Required:    true,
								},
								"private_key_id": {
									Description: "Private Key ID",
									Type:        types.StringType,
									Required:    true,
								},
								"client_email": {
									Description: "Client Email",
									Type:        types.StringType,
									Required:    true,
								},
								"client_id": {
									Description: "Client ID",
									Type:        types.StringType,
									Required:    true,
								},
								"auth_uri": {
									Description: "Auth URI",
									Type:        types.StringType,
									Required:    true,
								},
								"token_uri": {
									Description: "Token URI",
									Type:        types.StringType,
									Required:    true,
								},
								"auth_provider_x509_cert_url": {
									Description: "Auth Provider X509 Cert URL",
									Type:        types.StringType,
									Required:    true,
								},
								"client_x509_cert_url": {
									Description: "Client X509 Cert URL",
									Type:        types.StringType,
									Required:    true,
								},
								"universe_domain": {
									Description: "Google Universe Domain",
									Type:        types.StringType,
									Optional:    true,
								},
							}),
						},
					}),
				},
				"azure_cmk_spec": {
					Description: "AZURE CMK Provider Configuration.",
					Optional:    true,
					Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
						"client_id": {
							Description: "Azure Active Directory (AD) Client ID for Key Vault service principal.",
							Type:        types.StringType,
							Required:    true,
						},
						"client_secret": {
							Description: "Azure AD Client Secret for Key Vault service principal.",
							Type:        types.StringType,
							Required:    true,
						},
						"tenant_id": {
							Description: "Azure AD Tenant ID for Key Vault service principal.",
							Type:        types.StringType,
							Required:    true,
						},
						"key_vault_uri": {
							Description: "URI of Azure Key Vault storing cryptographic keys.",
							Type:        types.StringType,
							Required:    true,
						},
						"key_name": {
							Description: "Name of cryptographic key in Azure Key Vault.",
							Type:        types.StringType,
							Required:    true,
						},
					}),
				},
			}),
		},
		"cluster_tier": {
			Description: "FREE (Sandbox) or PAID (Dedicated).",
			Type:        types.StringType,
			Required:    true,
			Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("FREE", "PAID")},
		},
		"fault_tolerance": {
			Description: "The fault tolerance of the cluster. NONE, NODE, ZONE or REGION.",
			Type:        types.StringType,
			Optional:    true,
			Computed:    true,
			Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("NONE", "NODE", "ZONE", "REGION")},
		},
		"num_faults_to_tolerate": {
			Description: "The number of domain faults the cluster can tolerate. 0 for NONE, 1 for ZONE and [1-3] for NODE and REGION",
			Type:        types.Int64Type,
			Optional:    true,
			Computed:    true,
			Validators:  []tfsdk.AttributeValidator{int64validator.OneOf(0, 1, 2, 3)},
		},
		"cluster_allow_list_ids": {
			Description: "List of IDs of the allow lists assigned to the cluster.",
			Type: types.ListType{
				ElemType: types.StringType,
			},
			Optional: true,
		},
		"restore_backup_id": {
			Description: "The ID of the backup to be restored to the cluster.",
			Type:        types.StringType,
			Optional:    true,
		},
		"node_config": {
			Optional: true,
			Computed: true,
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"num_cores": {
					Description: "Number of CPU cores in the node.",
					Type:        types.Int64Type,
					Optional:    true,
					Computed:    true,
				},
				"disk_size_gb": {
					Description: "Disk size of the node.",
					Type:        types.Int64Type,
					Computed:    true,
					Optional:    true,
					Validators: []tfsdk.AttributeValidator{
						schemavalidator.ConflictsWith(
							path.MatchRelative().AtParent().AtParent().AtName("cluster_region_info").AtAnyListIndex().AtName("disk_size_gb"),
						),
					},
				},
				"disk_iops": {
					Description: "Disk IOPS of the node.",
					Type:        types.Int64Type,
					Computed:    true,
					Optional:    true,
					Validators: []tfsdk.AttributeValidator{
						schemavalidator.ConflictsWith(
							path.MatchRelative().AtParent().AtParent().AtName("cluster_region_info").AtAnyListIndex().AtName("disk_iops"),
						),
					},
				},
			}),
			DeprecationMessage: "Remove this attribute's configuration as it's no longer in use and the attribute will be removed in the next major version of the provider. Please use cluster_region_info to specify node config instead.",
		},
		"credentials": {
			Description: `Credentials to be used by the database. Please provide 'username' and 'password' 
(which would be used in common for both YSQL and YCQL) OR all of 'ysql_username',
'ysql_password', 'ycql_username' and 'ycql_password' but not a mix of both.`,
			Required: true,
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"username": {
					Description: "The username to be used for both YSQL and YCQL.",
					Type:        types.StringType,
					Optional:    true,
				},
				"password": {
					Description: "The password to be used for both YSQL and YCQL. Note that this will be stored in the state file.",
					Type:        types.StringType,
					Optional:    true,
					Sensitive:   true,
				},
				"ysql_username": {
					Description: "YSQL username for the database.",
					Type:        types.StringType,
					Optional:    true,
				},
				"ysql_password": {
					Description: "YSQL password for the database. Note that this will be stored in the state file.",
					Type:        types.StringType,
					Optional:    true,
					Sensitive:   true,
				},
				"ycql_username": {
					Description: "YCQL username for the database.",
					Type:        types.StringType,
					Optional:    true,
				},
				"ycql_password": {
					Description: "YCQL password for the database. Note that this will be stored in the state file.",
					Type:        types.StringType,
					Optional:    true,
					Sensitive:   true,
				},
			}),
		},
		"cluster_info": {
			Computed: true,
			Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
				"state": {
					Type:     types.StringType,
					Computed: true,
				},
				"software_version": {
					Type:     types.StringType,
					Computed: true,
				},
				"created_time": {
					Type:     types.StringType,
					Computed: true,
				},
				"updated_time": {
					Type:     types.StringType,
					Computed: true,
				},
			}),
		},
		"cluster_version": {
			Type:     types.StringType,
			Computed: true,
		},
		"database_track": {
			Description: "The track of the database. Production or Innovation or Preview.",
			Type:        types.StringType,
			Optional:    true,
			Computed:    true,
			PlanModifiers: []tfsdk.AttributePlanModifier{
				tfsdk.UseStateForUnknown(),
			},
		},
		"desired_state": {
			Description: "The desired state of the cluster, Active or Paused. This parameter can be used to pause/resume a cluster.",
			Type:        types.StringType,
			Optional:    true,
			Computed:    true,
			PlanModifiers: []tfsdk.AttributePlanModifier{
				tfsdk.UseStateForUnknown(),
			},
			Validators: []tfsdk.AttributeValidator{
				// Validate string value must be "Active" or "Paused"
				stringvalidator.OneOfCaseInsensitive([]string{"Active", "Paused"}...),
			},
		},
		"desired_connection_pooling_state": {
			Description: "The desired connection pooling state of the cluster, Enabled or Disabled. This parameter can be used to enable/disable Connection Pooling",
			Type:        types.StringType,
			Optional:    true,
			Computed:    true,
			PlanModifiers: []tfsdk.AttributePlanModifier{
				tfsdk.UseStateForUnknown(),
			},
			Validators: []tfsdk.AttributeValidator{
				// Validate string value must be "Enabled" or "Disabled"
				stringvalidator.OneOfCaseInsensitive([]string{"Enabled", "Disabled"}...),
			},
		},
		"cluster_endpoints": {
			Description:        "The endpoints used to connect to the cluster.",
			DeprecationMessage: "This attribute is deprecated. Please use the 'endpoints' attribute instead.",
			Type: types.MapType{
				ElemType: types.StringType,
			},
			Computed: true,
		},
		"endpoints": {
			Description: "The endpoints used to connect to the cluster.",
			Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
				"accessibility_type": {
					Description: "The accessibility type of the endpoint. PUBLIC or PRIVATE.",
					Type:        types.StringType,
					Computed:    true,
					Optional:    true,
				},
				"host": {
					Description: "The host of the endpoint.",
					Type:        types.StringType,
					Computed:    true,
					Optional:    true,
				},
				"region": {
					Description: "The region of the endpoint.",
					Type:        types.StringType,
					Computed:    true,
					Optional:    true,
				},
			}),
			Computed: true,
		},
		"cluster_certificate": {
			Description: "The certificate used to connect to the cluster.",
			Type:        types.StringType,
			Computed:    true,
			PlanModifiers: []tfsdk.AttributePlanModifier{
				tfsdk.UseStateForUnknown(),
			},
		},
	}
	// Remove once feature flag is enabled
	// TODO: Think of a more scalable solution
	if !fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) {
		delete(attributes, "desired_connection_pooling_state")
	}

	return tfsdk.Schema{
		Description: `The resource to create a YugabyteDB cluster. Use this resource to create both 
single- and multi-region clusters. You can also use this resource to bind allow lists to the cluster 
being created; restore previously taken backups to the cluster being created; 
and modify the backup schedule of the cluster being created.`,
		Attributes: attributes,
	}, nil
}

func (r resourceClusterType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceCluster{
		p: *(p.(*provider)),
	}, nil
}

type resourceCluster struct {
	p provider
}

func EditBackupSchedule(ctx context.Context, backupScheduleStruct BackupScheduleInfo, scheduleId string, backupDes string, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) error {
	return editBackupScheduleV2(ctx, backupScheduleStruct, scheduleId, backupDes, accountId, projectId, clusterId, apiClient)
}

func editBackupScheduleV2(ctx context.Context, backupScheduleStruct BackupScheduleInfo, scheduleId string, backupDes string, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) error {
	if backupScheduleStruct.State.Value != "" && backupScheduleStruct.RetentionPeriodInDays.Value != 0 {
		backupRetentionPeriodInDays := int32(backupScheduleStruct.RetentionPeriodInDays.Value)
		backupScheduleSpec := *openapiclient.NewScheduleSpecV2WithDefaults()
		backupScheduleSpec.SetDescription(backupDes)
		backupScheduleSpec.SetRetentionPeriodInDays(backupRetentionPeriodInDays)
		backupScheduleSpec.SetState(openapiclient.ScheduleStateEnum(backupScheduleStruct.State.Value))
		if !backupScheduleStruct.IncrementalIntervalInMins.IsNull() && !backupScheduleStruct.IncrementalIntervalInMins.IsUnknown() && backupScheduleStruct.IncrementalIntervalInMins.Value != 0 {
			incrementalIntervalInMins := int32(backupScheduleStruct.IncrementalIntervalInMins.Value)
			backupScheduleSpec.SetIncrementalIntervalInMinutes(incrementalIntervalInMins)
		} else {
			backupScheduleSpec.UnsetIncrementalIntervalInMinutes()
		}
		if !backupScheduleStruct.TimeIntervalInDays.IsNull() && !backupScheduleStruct.TimeIntervalInDays.IsUnknown() && backupScheduleStruct.TimeIntervalInDays.Value != 0 {
			timeIntervalInDays := int32(backupScheduleStruct.TimeIntervalInDays.Value)
			backupScheduleSpec.SetTimeIntervalInDays(timeIntervalInDays)
			backupScheduleSpec.UnsetCronExpression()
		}
		if backupScheduleStruct.CronExpression.Value != "" {
			cronExp := backupScheduleStruct.CronExpression.Value
			backupScheduleSpec.SetCronExpression(cronExp)
			backupScheduleSpec.UnsetTimeIntervalInDays()
		}
		if backupScheduleStruct.TimeIntervalInDays.Value != 0 && backupScheduleStruct.CronExpression.Value != "" {
			return errors.New("unable to create custom backup schedule. You can't pass both the cron expression and time interval in days")
		}

		_, res, err := apiClient.BackupApi.ModifyBackupScheduleV2(ctx, accountId, projectId, clusterId, scheduleId).ScheduleSpecV2(backupScheduleSpec).Execute()
		if err != nil {
			errMsg := getErrorMessage(res, err)
			return errors.New("Unable to modify the backup schedule. " + errMsg)
		}
	}
	return nil
}

func createClusterSpec(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, plan Cluster, state Cluster, clusterExists bool) (clusterSpec *openapiclient.ClusterSpec, clusterSpecOK bool, errorMessage string) {

	var diskSizeGb int32
	var diskSizeOK bool
	var memoryMb int32
	var memoryOK bool
	var trackId string
	var trackName string
	var trackIdOK bool
	var message string

	// Compute track ID for database version
	softwareInfo := *openapiclient.NewSoftwareInfoWithDefaults()
	if !plan.DatabaseTrack.Unknown {
		trackName = plan.DatabaseTrack.Value
		trackId, trackIdOK, message = getTrackId(ctx, apiClient, accountId, trackName)
		if !trackIdOK {
			return nil, false, message
		}
		softwareInfo.SetTrackId(trackId)
	}

	var clusterRegionInfo []openapiclient.ClusterRegionInfo
	totalNodes := 0
	clusterType := plan.ClusterType.Value
	isDefaultSet := false
	pseInfoMap := make(map[string]openapiclient.PrivateServiceEndpointRegionSpec)
	if clusterExists {
		clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, state.ClusterID.Value).Execute()
		if err != nil {
			errMsg := getErrorMessage(response, err)
			return nil, false, errMsg
		}
		for _, regionInfo := range clusterResp.Data.Spec.ClusterRegionInfo {
			if slices.Contains(regionInfo.GetAccessibilityTypes(), openapiclient.ACCESSIBILITYTYPE_PRIVATE_SERVICE_ENDPOINT) {
				pseInfoMap[regionInfo.PlacementInfo.CloudInfo.GetRegion()] = regionInfo.GetPrivateServiceEndpointInfo()
			}

		}
	}
	tflog.Debug(ctx, fmt.Sprintf("PSE info map is %v", pseInfoMap))

	for _, regionInfo := range plan.ClusterRegionInfo {
		regionNodes := regionInfo.NumNodes.Value
		totalNodes += int(regionNodes)
		info := *openapiclient.NewClusterRegionInfo(
			*openapiclient.NewPlacementInfo(
				*openapiclient.NewCloudInfo(
					openapiclient.CloudEnum(plan.CloudType.Value),
					regionInfo.Region.Value), int32(regionNodes)),
		)
		if vpcName := regionInfo.VPCName.Value; vpcName != "" {
			vpcData, err := getVPCByName(context.Background(), accountId, projectId, vpcName, apiClient)
			if err != nil {
				return nil, false, err.Error()
			}

			regionInfo.VPCID.Value = vpcData.Info.Id
		}

		cloud := plan.CloudType.Value
		tier := plan.ClusterTier.Value
		region := regionInfo.Region.Value
		var numCores int32
		// If regionInfo numCores is null, then it must be coming from the root level node info.
		if regionInfo.NumCores.IsNull() || regionInfo.NumCores.IsUnknown() {
			numCores = int32(plan.NodeConfig.NumCores.Value)
		} else {
			numCores = int32(regionInfo.NumCores.Value)
		}

		memoryMb, memoryOK, message = getMemoryFromInstanceType(ctx, apiClient, accountId, cloud, tier, region, numCores)
		if !memoryOK {
			return nil, false, message
		}

		if !regionInfo.DiskSizeGb.IsUnknown() && !regionInfo.DiskSizeGb.IsNull() {
			diskSizeGb = int32(regionInfo.DiskSizeGb.Value)
		} else if plan.NodeConfig != nil && !plan.NodeConfig.DiskSizeGb.IsUnknown() && !plan.NodeConfig.DiskSizeGb.IsNull() {
			diskSizeGb = int32(plan.NodeConfig.DiskSizeGb.Value)
		} else {
			diskSizeGb, diskSizeOK, message = getDiskSizeFromInstanceType(ctx, apiClient, accountId, cloud, tier, region, numCores)
			if !diskSizeOK {
				return nil, false, message
			}
		}

		nodeInfo := *openapiclient.NewOptionalClusterNodeInfo(numCores, memoryMb, diskSizeGb)
		if !regionInfo.DiskIops.IsUnknown() && !regionInfo.DiskIops.IsNull() && int32(regionInfo.DiskIops.Value) > 0 {
			nodeInfo.SetDiskIops(int32(regionInfo.DiskIops.Value))
		} else if plan.NodeConfig != nil && !plan.NodeConfig.DiskIops.IsNull() && !plan.NodeConfig.DiskIops.IsNull() && int32(plan.NodeConfig.DiskIops.Value) > 0 {
			nodeInfo.SetDiskIops(int32(plan.NodeConfig.DiskIops.Value))
		} else {
			nodeInfo.UnsetDiskIops()
		}

		info.SetNodeInfo(nodeInfo)

		// Create an array of AccessibilityType and populate it according to
		// the following logic:
		// if the cluster is in a private VPC, it MUST always have PRIVATE.
		// if the cluster is NOT in a private VPC, it MUST always have PUBLIC.
		// if the cluster is in a private VPC and customer wants public access, it MUST have PRIVATE and PUBLIC.
		var accessibilityTypes []openapiclient.AccessibilityType

		if vpcID := regionInfo.VPCID.Value; vpcID != "" {
			info.PlacementInfo.SetVpcId(vpcID)
			accessibilityTypes = append(accessibilityTypes, openapiclient.ACCESSIBILITYTYPE_PRIVATE)

			if regionInfo.PublicAccess.Value {
				accessibilityTypes = append(accessibilityTypes, openapiclient.ACCESSIBILITYTYPE_PUBLIC)
			}
		} else {
			accessibilityTypes = append(accessibilityTypes, openapiclient.ACCESSIBILITYTYPE_PUBLIC)

			// If the value is specified, and it is false, then it is an error because the user
			// wants disabled public access on a non-dedicated VPC cluster.
			if !regionInfo.PublicAccess.IsUnknown() && !regionInfo.PublicAccess.Value {
				tflog.Debug(ctx, fmt.Sprintf("Cluster %v is in a public VPC and public access is disabled. ", plan.ClusterName.Value))
				return nil, false, "Cluster is in a public VPC and public access is disabled. Please enable public access."
			}
		}

		if clusterExists {
			if pseEndpointSpec, exists := pseInfoMap[regionInfo.Region.Value]; exists {
				accessibilityTypes = append(accessibilityTypes, openapiclient.ACCESSIBILITYTYPE_PRIVATE_SERVICE_ENDPOINT)
				info.SetPrivateServiceEndpointInfo(pseEndpointSpec)
			}
		}

		// Set the accessibility type for the region
		info.SetAccessibilityTypes(accessibilityTypes)

		if clusterType == "SYNCHRONOUS" {
			info.PlacementInfo.SetMultiZone(false)
		}
		info.SetIsDefault(false)
		info.SetIsAffinitized(false)
		if !regionInfo.IsPreferred.IsUnknown() && !regionInfo.IsPreferred.IsNull() {
			info.SetIsAffinitized(regionInfo.IsPreferred.Value)
		}

		if !regionInfo.IsDefault.IsUnknown() && !regionInfo.IsDefault.IsNull() && regionInfo.IsDefault.Value {
			if isDefaultSet {
				return nil, false, "Cluster must have exactly one default region."
			}
			info.SetIsDefault(regionInfo.IsDefault.Value)
			isDefaultSet = true
		}

		clusterRegionInfo = append(clusterRegionInfo, info)
	}

	// This is to pass in the region information to fetch memory and disk size
	regionCount := len(clusterRegionInfo)
	if regionCount == 1 {
		clusterRegionInfo[0].SetIsDefault(true)
	}

	// This is to support a redundant value in the API.
	// Needs to be removed once API cleans it up.
	isProduction := true
	if plan.ClusterTier.Value == "FREE" {
		isProduction = false
	}

	clusterInfo := *openapiclient.NewClusterInfo(
		openapiclient.ClusterTier(plan.ClusterTier.Value),
		int32(totalNodes),
		openapiclient.ClusterFaultTolerance(plan.FaultTolerance.Value),
		isProduction,
	)

	clusterInfo.UnsetNodeInfo()

	if !plan.NumFaultsToTolerate.IsUnknown() {
		clusterInfo.SetNumFaultsToTolerate(int32(plan.NumFaultsToTolerate.Value))
	}

	clusterInfo.SetClusterType(openapiclient.ClusterType(clusterType))
	if clusterExists {
		clusterVersion, _ := strconv.Atoi(plan.ClusterVersion.Value)
		clusterInfo.SetVersion(int32(clusterVersion))
	}

	clusterSpec = openapiclient.NewClusterSpec(
		plan.ClusterName.Value,
		clusterInfo,
		softwareInfo)

	clusterSpec.SetClusterRegionInfo(clusterRegionInfo)

	return clusterSpec, true, ""
}

func getPlan(ctx context.Context, plan tfsdk.Plan, cluster *Cluster) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &cluster)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &cluster.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_name"), &cluster.ClusterName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cloud_type"), &cluster.CloudType)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_type"), &cluster.ClusterType)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_region_info"), &cluster.ClusterRegionInfo)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("fault_tolerance"), &cluster.FaultTolerance)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("num_faults_to_tolerate"), &cluster.NumFaultsToTolerate)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_tier"), &cluster.ClusterTier)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_allow_list_ids"), &cluster.ClusterAllowListIDs)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("restore_backup_id"), &cluster.RestoreBackupID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("database_track"), &cluster.DatabaseTrack)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("desired_state"), &cluster.DesiredState)...)
	if fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) {
		diags.Append(plan.GetAttribute(ctx, path.Root("desired_connection_pooling_state"), &cluster.DesiredConnectionPoolingState)...)
	}

	var nodeConfig *NodeConfig
	plan.GetAttribute(ctx, path.Root("node_config"), &nodeConfig)
	if nodeConfig != nil {
		diags.Append(plan.GetAttribute(ctx, path.Root("node_config"), &cluster.NodeConfig)...)
	}

	diags.Append(plan.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("backup_schedules"), &cluster.BackupSchedules)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cmk_spec"), &cluster.CMKSpec)...)

	return diags
}

// fills account, project, cluster ID from state
func getIDsFromState(ctx context.Context, state tfsdk.State, cluster *Cluster) {
	state.GetAttribute(ctx, path.Root("account_id"), &cluster.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &cluster.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &cluster.ClusterID)
	state.GetAttribute(ctx, path.Root("desired_state"), &cluster.DesiredState)
	if fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) {
		state.GetAttribute(ctx, path.Root("desired_connection_pooling_state"), &cluster.DesiredConnectionPoolingState)
	}
	state.GetAttribute(ctx, path.Root("cluster_allow_list_ids"), &cluster.ClusterAllowListIDs)
	state.GetAttribute(ctx, path.Root("cluster_region_info"), &cluster.ClusterRegionInfo)
	state.GetAttribute(ctx, path.Root("backup_schedules"), &cluster.BackupSchedules)
}

func validateCredentials(credentials Credentials) bool {

	commonCredentialsProvided := !credentials.Username.IsNull() && !credentials.Password.IsNull()
	commonCredentialsNotProvided := credentials.Username.IsNull() && credentials.Password.IsNull()
	ysqlCredentialsProvided := !credentials.YSQLUsername.IsNull() && !credentials.YSQLPassword.IsNull()
	ysqlCredentialsNotProvided := credentials.YSQLUsername.IsNull() && credentials.YSQLPassword.IsNull()
	ycqlCredentialsProvided := !credentials.YCQLUsername.IsNull() && !credentials.YCQLPassword.IsNull()
	ycqlCredentialsNotProvided := credentials.YCQLUsername.IsNull() && credentials.YCQLPassword.IsNull()

	if (commonCredentialsProvided && ysqlCredentialsNotProvided && ycqlCredentialsNotProvided) ||
		(ysqlCredentialsProvided && ycqlCredentialsProvided && commonCredentialsNotProvided) {
		return true
	}

	return false

}

// This function is needed to fix deserialization into TF state when connection pooling is removed from schema
func setClusterState(ctx context.Context, state *tfsdk.State, cluster *Cluster) diag.Diagnostics {
	// Create temporary struct without DesiredConnectionPoolingState field
	if fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) {
		return state.Set(ctx, cluster)
	}
	tempState := struct {
		AccountID           types.String         `tfsdk:"account_id"`
		ProjectID           types.String         `tfsdk:"project_id"`
		ClusterID           types.String         `tfsdk:"cluster_id"`
		ClusterName         types.String         `tfsdk:"cluster_name"`
		CloudType           types.String         `tfsdk:"cloud_type"`
		ClusterType         types.String         `tfsdk:"cluster_type"`
		FaultTolerance      types.String         `tfsdk:"fault_tolerance"`
		NumFaultsToTolerate types.Int64          `tfsdk:"num_faults_to_tolerate"`
		ClusterRegionInfo   []RegionInfo         `tfsdk:"cluster_region_info"`
		DatabaseTrack       types.String         `tfsdk:"database_track"`
		DesiredState        types.String         `tfsdk:"desired_state"`
		ClusterTier         types.String         `tfsdk:"cluster_tier"`
		ClusterAllowListIDs []types.String       `tfsdk:"cluster_allow_list_ids"`
		RestoreBackupID     types.String         `tfsdk:"restore_backup_id"`
		NodeConfig          *NodeConfig          `tfsdk:"node_config"`
		Credentials         Credentials          `tfsdk:"credentials"`
		ClusterInfo         ClusterInfo          `tfsdk:"cluster_info"`
		ClusterVersion      types.String         `tfsdk:"cluster_version"`
		BackupSchedules     []BackupScheduleInfo `tfsdk:"backup_schedules"`
		ClusterEndpoints    types.Map            `tfsdk:"cluster_endpoints"`
		ClusterEndpointsV2  []ClusterEndpoint    `tfsdk:"endpoints"`
		ClusterCertificate  types.String         `tfsdk:"cluster_certificate"`
		CMKSpec             *CMKSpec             `tfsdk:"cmk_spec"`
	}{
		AccountID:           cluster.AccountID,
		ProjectID:           cluster.ProjectID,
		ClusterID:           cluster.ClusterID,
		ClusterName:         cluster.ClusterName,
		CloudType:           cluster.CloudType,
		ClusterType:         cluster.ClusterType,
		FaultTolerance:      cluster.FaultTolerance,
		NumFaultsToTolerate: cluster.NumFaultsToTolerate,
		ClusterRegionInfo:   cluster.ClusterRegionInfo,
		DatabaseTrack:       cluster.DatabaseTrack,
		DesiredState:        cluster.DesiredState,
		ClusterTier:         cluster.ClusterTier,
		ClusterAllowListIDs: cluster.ClusterAllowListIDs,
		RestoreBackupID:     cluster.RestoreBackupID,
		NodeConfig:          cluster.NodeConfig,
		Credentials:         cluster.Credentials,
		ClusterInfo:         cluster.ClusterInfo,
		ClusterVersion:      cluster.ClusterVersion,
		BackupSchedules:     cluster.BackupSchedules,
		ClusterEndpoints:    cluster.ClusterEndpoints,
		ClusterEndpointsV2:  cluster.ClusterEndpointsV2,
		ClusterCertificate:  cluster.ClusterCertificate,
		CMKSpec:             cluster.CMKSpec,
	}

	return state.Set(ctx, &tempState)
}

func validateOnlyOneCMKSpec(plan *Cluster) error {
	count := 0

	if plan.CMKSpec.GCPCMKSpec != nil {
		count++
	}
	if plan.CMKSpec.AWSCMKSpec != nil {
		count++
	}
	if plan.CMKSpec.AzureCMKSpec != nil {
		count++
	}

	if count != 1 {
		return errors.New("invalid input. Only one CMK Provider out of AWS, GCP, or AZURE must be present")
	}

	return nil
}

func createCmkSpec(plan Cluster) (*openapiclient.CMKSpec, error) {
	cmkProvider := plan.CMKSpec.ProviderType.Value
	cmkSpec := openapiclient.NewCMKSpec(openapiclient.CMKProviderEnum(cmkProvider))

	if err := validateOnlyOneCMKSpec(&plan); err != nil {
		return nil, err
	}

	switch cmkProvider {
	case "GCP":
		if plan.CMKSpec.GCPCMKSpec == nil {
			return nil, errors.New("provider type is GCP but GCP CMK spec is missing")
		}
		gcpKeyRingName := plan.CMKSpec.GCPCMKSpec.KeyRingName.Value
		gcpKeyName := plan.CMKSpec.GCPCMKSpec.KeyName.Value
		gcpLocation := plan.CMKSpec.GCPCMKSpec.Location.Value
		gcpProtectionLevel := plan.CMKSpec.GCPCMKSpec.ProtectionLevel.Value
		gcpServiceAccount := plan.CMKSpec.GCPCMKSpec.GcpServiceAccount
		gcpServiceAccountSpec := openapiclient.NewGCPServiceAccount(
			gcpServiceAccount.Type.Value,
			gcpServiceAccount.ProjectId.Value,
			"",
			gcpServiceAccount.PrivateKeyId.Value,
			gcpServiceAccount.ClientEmail.Value,
			gcpServiceAccount.ClientId.Value,
			gcpServiceAccount.AuthUri.Value,
			gcpServiceAccount.TokenUri.Value,
			gcpServiceAccount.AuthProviderX509CertUrl.Value,
			gcpServiceAccount.ClientX509CertUrl.Value,
		)
		if !gcpServiceAccount.PrivateKey.Null && !gcpServiceAccount.PrivateKey.Unknown && gcpServiceAccount.PrivateKey.Value != "" {
			gcpServiceAccountSpec.SetPrivateKey(gcpServiceAccount.PrivateKey.Value)
		}
		if !gcpServiceAccount.UniverseDomain.Null && !gcpServiceAccount.UniverseDomain.Unknown && gcpServiceAccount.UniverseDomain.Value != "" {
			gcpServiceAccountSpec.SetUniverseDomain(gcpServiceAccount.UniverseDomain.Value)
		}
		gcpCmkSpec := openapiclient.NewGCPCMKSpec(gcpKeyRingName, gcpKeyName, gcpLocation, gcpProtectionLevel)
		gcpCmkSpec.SetGcpServiceAccount(*gcpServiceAccountSpec)
		cmkSpec.SetGcpCmkSpec(*gcpCmkSpec)
	case "AWS":
		if plan.CMKSpec.AWSCMKSpec == nil {
			return nil, errors.New("provider type is AWS but AWS CMK spec is missing")
		}
		awsSecretKey := plan.CMKSpec.AWSCMKSpec.SecretKey.Value
		awsAccessKey := plan.CMKSpec.AWSCMKSpec.AccessKey.Value
		awsArnList := make([]string, len(plan.CMKSpec.AWSCMKSpec.ARNList))

		for i, arn := range plan.CMKSpec.AWSCMKSpec.ARNList {
			awsArnList[i] = arn.Value
		}

		awsCmkSpec := openapiclient.NewAWSCMKSpec(awsAccessKey, awsSecretKey, awsArnList)
		cmkSpec.SetAwsCmkSpec(*awsCmkSpec)
	case "AZURE":
		if plan.CMKSpec.AzureCMKSpec == nil {
			return nil, errors.New("provider type is AZURE but AZURE CMK spec is missing")
		}
		azureClientId := plan.CMKSpec.AzureCMKSpec.ClientID.Value
		azureClientSecret := plan.CMKSpec.AzureCMKSpec.ClientSecret.Value
		azureTenantId := plan.CMKSpec.AzureCMKSpec.TenantID.Value
		azureKeyVaultUri := plan.CMKSpec.AzureCMKSpec.KeyVaultUri.Value
		azureKeyName := plan.CMKSpec.AzureCMKSpec.KeyName.Value

		azureCmkSpec := openapiclient.NewAzureCMKSpec(azureClientId, azureClientSecret, azureTenantId, azureKeyVaultUri, azureKeyName)
		cmkSpec.SetAzureCmkSpec(*azureCmkSpec)
	}

	cmkSpec.SetIsEnabled(plan.CMKSpec.IsEnabled.Value)

	return cmkSpec, nil
}

// Create a new resource
func (r resourceCluster) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}
	var accountId, message string
	var plan Cluster
	var getAccountOK bool
	resp.Diagnostics.Append(getPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Cluster Resource: Error on Get Plan")
		return
	}

	if !validateCredentials(plan.Credentials) {
		resp.Diagnostics.AddError("Invalid credentials", `Please provide 'username' and 'password' 
(which would be used in common for both YSQL and YCQL) OR all of 'ysql_username',
'ysql_password', 'ycql_username' and 'ycql_password' but not a mix of both.`)
		return
	}

	if plan.NodeConfig != nil && !plan.NodeConfig.DiskSizeGb.IsNull() && !plan.NodeConfig.DiskSizeGb.IsUnknown() && !util.IsDiskSizeValid(plan.ClusterTier.Value, plan.NodeConfig.DiskSizeGb.Value) {
		resp.Diagnostics.AddError("Invalid disk size", "The disk size for a paid cluster must be at least 50 GB.")
		return
	}

	if plan.NodeConfig != nil && !plan.NodeConfig.DiskIops.IsNull() && !plan.NodeConfig.DiskIops.IsUnknown() {
		isValid, err := util.IsDiskIopsValid(plan.CloudType.Value, plan.ClusterTier.Value, plan.NodeConfig.DiskIops.Value)
		if !isValid {
			resp.Diagnostics.AddError("Invalid disk IOPS", err)
			return
		}
	}

	backupId := ""
	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	if !plan.ClusterID.Unknown && !plan.ClusterID.Null && plan.ClusterID.Value != "" {
		resp.Diagnostics.AddError(
			"Cluster ID provided for new cluster",
			"The cluster_id was provided even though a new cluster is being created. Do not include this field in the provider when creating a cluster.",
		)
		return
	}

	for _, regionInfo := range plan.ClusterRegionInfo {
		vpcNamePresent := false
		vpcIDPresent := false
		if !regionInfo.VPCName.Unknown && !regionInfo.VPCName.Null && regionInfo.VPCName.Value != "" {
			vpcNamePresent = true
		}
		if !regionInfo.VPCID.Unknown && !regionInfo.VPCID.Null && regionInfo.VPCID.Value != "" {
			vpcIDPresent = true
		}
		if vpcNamePresent {
			if vpcIDPresent {
				resp.Diagnostics.AddError(
					"Specify VPC name or VPC ID",
					"To select a vpc, use either vpc_name or vpc_id. Don't provide both.",
				)
				return
			}
		}

		if !regionInfo.DiskSizeGb.IsUnknown() && !util.IsDiskSizeValid(plan.ClusterTier.Value, regionInfo.DiskSizeGb.Value) {
			resp.Diagnostics.AddError("Invalid disk size in "+regionInfo.Region.Value, "The disk size for a paid cluster must be at least 50 GB.")
			return
		}

		if !regionInfo.DiskIops.IsUnknown() && !regionInfo.DiskIops.IsNull() {
			isValid, err := util.IsDiskIopsValid(plan.CloudType.Value, plan.ClusterTier.Value, regionInfo.DiskIops.Value)
			if !isValid {
				resp.Diagnostics.AddError("Invalid disk IOPS in "+regionInfo.Region.Value, err)
				return
			}
		}
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	clusterSpec, clusterOK, message := createClusterSpec(ctx, apiClient, accountId, projectId, plan, Cluster{}, false)
	if !clusterOK {
		resp.Diagnostics.AddError("Unable to create cluster spec", message)
		return
	}

	createClusterRequest := *openapiclient.NewCreateClusterRequest(*clusterSpec)

	encryptedCredentials := openapiclient.NewCreateClusterRequestEncryptedDbCredentialsWithDefaults()
	if plan.Credentials.Username.IsNull() {
		encryptedCredentials.SetYsql(*openapiclient.NewEncryptedDBCredentials(b64(plan.Credentials.YSQLUsername.Value), b64(plan.Credentials.YSQLPassword.Value)))
		encryptedCredentials.SetYcql(*openapiclient.NewEncryptedDBCredentials(b64(plan.Credentials.YCQLUsername.Value), b64(plan.Credentials.YCQLPassword.Value)))
	} else {
		encryptedCredentials.SetYsql(*openapiclient.NewEncryptedDBCredentials(b64(plan.Credentials.Username.Value), b64(plan.Credentials.Password.Value)))
		encryptedCredentials.SetYcql(*openapiclient.NewEncryptedDBCredentials(b64(plan.Credentials.Username.Value), b64(plan.Credentials.Password.Value)))
	}
	createClusterRequest.SetEncryptedDbCredentials(*encryptedCredentials)

	var cmkSpec *openapiclient.CMKSpec

	if plan.CMKSpec != nil {
		// EAR disabled is not supported with cluster creation
		if !plan.CMKSpec.IsEnabled.Value {
			resp.Diagnostics.AddError(
				"EAR will be enabled by default.", "Cluster creation with EAR disabled is not supported.",
			)
		}
		var err error
		cmkSpec, err = createCmkSpec(plan)

		if err == nil {
			createClusterRequest.SecurityCmkSpec = *openapiclient.NewNullableCMKSpec(cmkSpec)
		} else {
			resp.Diagnostics.AddError("Error creating CMK Spec.", err.Error())
			return
		}
	}

	clusterResp, response, err := apiClient.ClusterApi.CreateCluster(ctx, accountId, projectId).CreateClusterRequest(createClusterRequest).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			resp.Diagnostics.AddError("Could not create cluster. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows:",
				errMsg[:10000])
			return
		}
		resp.Diagnostics.AddError("Could not create cluster", errMsg)
		return
	}
	clusterId := clusterResp.Data.Info.Id
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_CREATE_CLUSTER, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("the cluster creation is in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for cluster creation to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "cluster creation operation failed"
		}
		resp.Diagnostics.AddError("Unable to create cluster:", msg)
		return
	}

	// read status, wait for status to be done
	retryPolicyA := retry.NewConstant(10 * time.Second)
	retryPolicyA = retry.WithMaxDuration(3600*time.Second, retryPolicyA)
	readClusterRetries = 0
	err = retry.Do(ctx, retryPolicyA, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") || clusterState == "Create Failed" || clusterState == "CREATE_FAILED" {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("the cluster creation is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create cluster ", "The operation timed out waiting for cluster creation.")
		return
	}

	// Backup_schedule
	scheduleId := ""
	description := ""
	var r1 *http.Response
	scheduleId, description, r1, err = getBackupScheduleInfoV2(ctx, apiClient, accountId, projectId, clusterId)

	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch the backup schedule for the cluster "+r1.Status, "Try again")
		return
	}

	var backUpSchedules []BackupScheduleInfo
	if plan.BackupSchedules != nil && len(plan.BackupSchedules) > 0 {
		if len(plan.BackupSchedules) > 1 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "More than one schedules were passed")
			return
		}

		if plan.BackupSchedules[0].State.Value != "" && plan.BackupSchedules[0].RetentionPeriodInDays.Value == 0 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "Pass both state and retention period in days ")
			return
		}
		if plan.BackupSchedules[0].State.Value == "" && plan.BackupSchedules[0].RetentionPeriodInDays.Value != 0 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "Pass both state and retention period in days ")
			return
		}

		//Edit Backup Schedule
		tflog.Info(ctx, fmt.Sprintf("User defined description '%v' default description '%v'", plan.BackupSchedules[0].BackupDescription.Value, description))
		newDescription := ""
		if plan.BackupSchedules[0].BackupDescription.Value == "" {
			newDescription = description
		} else {
			newDescription = plan.BackupSchedules[0].BackupDescription.Value
		}
		err = EditBackupSchedule(ctx, plan.BackupSchedules[0], scheduleId, newDescription, accountId, projectId, clusterId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error duing store: ", err.Error())
			return
		}

		backupScheduleStruct := BackupScheduleInfo{
			ScheduleID: types.String{Value: scheduleId},
		}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)
	}
	if plan.BackupSchedules != nil && len(plan.BackupSchedules) == 0 {
		backupScheduleStruct := BackupScheduleInfo{}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)

	}

	var allowListIDs []string
	allowListProvided := false
	if plan.ClusterAllowListIDs != nil {
		for i := range plan.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, plan.ClusterAllowListIDs[i].Value)
		}

		tflog.Debug(ctx, fmt.Sprintf("Updating cluster with cluster ID %v with allow lists %v", clusterId, allowListIDs))

		_, response, err := apiClient.ClusterApi.EditClusterNetworkAllowLists(ctx, accountId, projectId, clusterId).RequestBody(allowListIDs).Execute()
		if err != nil {
			errMsg := getErrorMessage(response, err)
			resp.Diagnostics.AddError("Unable to assign allow list to cluster", errMsg)
			return
		}
		allowListProvided = true
	}

	restoreRequired := false
	if (!plan.RestoreBackupID.Unknown && !plan.RestoreBackupID.Null) || plan.RestoreBackupID.Value != "" {
		restoreRequired = true
		backupId = plan.RestoreBackupID.Value
	}
	if restoreRequired {
		err = handleRestore(ctx, accountId, projectId, clusterId, backupId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error during store: ", err.Error())
			return
		}
	}

	var regions []string
	for _, regionInfo := range plan.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.Value)
	}

	// Pause the cluster if the desired state is set to 'Paused'
	if !plan.DesiredState.Unknown && strings.EqualFold(plan.DesiredState.Value, "Paused") {
		err := pauseCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Pausing the cluster Failed: ", err.Error())
		}
	}

	cluster, readOK, message := resourceClusterRead(ctx, accountId, projectId, clusterId, backUpSchedules, regions, allowListProvided, allowListIDs, false, apiClient)

	// Update the State file with the unmasked creds for AWS (secret key,access) and GCP (client id,private key)
	if plan.CMKSpec != nil {
		providerType := cluster.CMKSpec.ProviderType.Value
		switch providerType {
		case "AWS":
			cluster.CMKSpec.AWSCMKSpec.SecretKey = types.String{Value: cmkSpec.GetAwsCmkSpec().SecretKey}
			cluster.CMKSpec.AWSCMKSpec.AccessKey = types.String{Value: cmkSpec.GetAwsCmkSpec().AccessKey}
		case "GCP":
			if cmkSpec.GetGcpCmkSpec().GcpServiceAccount.IsSet() {
				gcpServiceAccountData := cmkSpec.GetGcpCmkSpec().GcpServiceAccount.Get()
				cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.ClientId = types.String{Value: gcpServiceAccountData.ClientId}
				if gcpServiceAccountData.GetPrivateKey() != "" {
					cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey = types.String{Value: gcpServiceAccountData.GetPrivateKey()}
				} else {
					cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey.Null = true
				}
			}
		case "AZURE":
			cluster.CMKSpec.AzureCMKSpec.ClientSecret = types.String{Value: cmkSpec.GetAzureCmkSpec().ClientSecret}
		}
	}

	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster ", message)
		return
	}

	// set credentials for cluster (not returned by read api)
	if plan.Credentials.Username.IsNull() {
		cluster.Credentials.YSQLUsername.Value = plan.Credentials.YSQLUsername.Value
		cluster.Credentials.YSQLPassword.Value = plan.Credentials.YSQLPassword.Value
		cluster.Credentials.YCQLUsername.Value = plan.Credentials.YCQLUsername.Value
		cluster.Credentials.YCQLPassword.Value = plan.Credentials.YCQLPassword.Value
		cluster.Credentials.Username.Null = true
		cluster.Credentials.Password.Null = true
	} else {
		// common credentials have been used
		cluster.Credentials.Username.Value = plan.Credentials.Username.Value
		cluster.Credentials.Password.Value = plan.Credentials.Password.Value
		cluster.Credentials.YSQLUsername.Null = true
		cluster.Credentials.YSQLPassword.Null = true
		cluster.Credentials.YCQLUsername.Null = true
		cluster.Credentials.YCQLPassword.Null = true
	}

	// set restore backup id for cluster (not returned by read api)
	if restoreRequired {
		cluster.RestoreBackupID.Value = plan.RestoreBackupID.Value
	} else {
		cluster.RestoreBackupID.Null = true
	}

	if fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) && (strings.EqualFold(plan.DesiredConnectionPoolingState.Value, "Enabled") || strings.EqualFold(plan.DesiredConnectionPoolingState.Value, "Disabled")) {
		cluster.DesiredConnectionPoolingState.Value = plan.DesiredConnectionPoolingState.Value
	}
	diags := setClusterState(ctx, &resp.State, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getBackupScheduleInfoV2(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (string, string, *http.Response, error) {

	scheduleResp, r, err := apiClient.BackupApi.ListBackupSchedulesV2(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return "", "", r, err
	}
	list := scheduleResp.GetData()
	scheduleId := list[0].GetInfo().Id
	spec := list[0].GetSpec()
	description := spec.GetDescription()
	return scheduleId, description, nil, nil
}

func pauseCluster(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (err error) {

	_, response, err := apiClient.ClusterApi.PauseCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not pause the cluster. " + errMsg[:10000])
		}
		return errors.New("Could not pause the cluster. " + errMsg)

	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Paused") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("the cluster is being paused"))
	})

	if err != nil {
		return errors.New("unable to pause cluster. " + "The operation timed out waiting to pause the cluster")
	}

	return nil

}

func enableConnectionPooling(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (err error) {
	connectionPoolingOpSpec := openapiclient.NewConnectionPoolingOpSpec(openapiclient.CONNECTIONPOOLINGOPENUM_ENABLE)
	response, err := apiClient.ClusterApi.PerformConnectionPoolingOperation(ctx, accountId, projectId, clusterId).ConnectionPoolingOpSpec(*connectionPoolingOpSpec).Execute()

	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not enable connection pooling " + errMsg[:10000])
		}
		return errors.New("Could not enable connection pooling. " + errMsg)

	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("connection Pooling is being enabled"))
	})

	if err != nil {
		return errors.New("unable to enable connection pooling " + "The operation timed out waiting to enable connection pooling")
	}

	return nil
}

func disableConnectionPooling(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (err error) {
	connectionPoolingOpSpec := openapiclient.NewConnectionPoolingOpSpec(openapiclient.CONNECTIONPOOLINGOPENUM_DISABLE)
	response, err := apiClient.ClusterApi.PerformConnectionPoolingOperation(ctx, accountId, projectId, clusterId).ConnectionPoolingOpSpec(*connectionPoolingOpSpec).Execute()

	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not disable connection pooling " + errMsg[:10000])
		}
		return errors.New("Could not disable connection pooling. " + errMsg)

	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("connection Pooling is being disabled"))
	})

	if err != nil {
		return errors.New("unable to disable connection pooling " + "The operation timed out waiting to disable connection pooling")
	}

	return nil
}

func editClusterCmk(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string, cmkSpec openapiclient.CMKSpec) (err error) {
	_, res, err := apiClient.ClusterApi.EditClusterCMK(context.Background(), accountId, projectId, clusterId).CMKSpec(cmkSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(res, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not edit the cluster CMK. " + errMsg[:10000])
		}
		return errors.New("Could not edit the cluster CMK. " + errMsg)
	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("cluster CMK is getting updated"))
	})

	if err != nil {
		return errors.New("unable to edit cluster CMK. " + "The operation timed out waiting to edit CMK")
	}

	return nil
}

func handleReadFailureWithRetries(ctx context.Context, readClusterRetries *int, maxRetries int, errMsg string) error {

	if *readClusterRetries < maxRetries {
		*readClusterRetries++
		tflog.Info(ctx, "Unable to get cluster state, retrying...")
		return retry.RetryableError(errors.New("unable to get cluster state, retrying"))
	}

	tflog.Info(ctx, "Unable to get cluster state, giving up...")
	return errors.New("Unable to get cluster state: " + errMsg)
}

func resumeCluster(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (err error) {

	_, response, err := apiClient.ClusterApi.ResumeCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not resume the cluster. " + errMsg[:10000])
		}
		return errors.New("Could not resume the cluster. " + errMsg)
	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("the cluster is being resumed"))
	})

	if err != nil {
		return errors.New("unable to resume cluster. " + "The operation timed out waiting to resume the cluster")
	}

	return nil

}

func getClusterState(ctx context.Context, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (state string, readInfoOK bool, errorMessage string) {
	clusterResp, resp, err := apiClient.ClusterApi.GetCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return "", false, errMsg
	}

	return string(clusterResp.Data.Info.GetState()), true, ""
}

func getRestoreState(ctx context.Context, accountId string, projectId string, clusterId string, backupId string, restoreId string, apiClient *openapiclient.APIClient) (state string, readInfoOK bool, errorMessage string) {
	restoreResp, resp, err := apiClient.BackupApi.GetRestore(ctx, accountId, projectId, restoreId).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return "", false, errMsg
	}
	//ListRestores(ctx, accountId, projectId).BackupId(backupId).ClusterId(clusterId).Execute()
	return string(restoreResp.Data.Info.GetState()), true, ""
}

// Read resource information
func (r resourceCluster) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state Cluster
	getIDsFromState(ctx, req.State, &state)

	var allowListIDs []string
	allowListProvided := false
	if state.ClusterAllowListIDs != nil {
		allowListProvided = true
		for i := range state.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, state.ClusterAllowListIDs[i].Value)
		}
	}

	var regions []string
	for _, regionInfo := range state.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.Value)
	}

	var backUpSchedules []BackupScheduleInfo
	if state.BackupSchedules != nil && len(state.BackupSchedules) > 0 {
		backUpSchedules = append(backUpSchedules, state.BackupSchedules[0])
	}

	cluster, readOK, message := resourceClusterRead(ctx, state.AccountID.Value, state.ProjectID.Value, state.ClusterID.Value, backUpSchedules, regions, allowListProvided, allowListIDs, false, r.p.client)

	// Fetch the cmkSpec information from State (to get unmasked creds)
	var cmkSpec CMKSpec
	req.State.GetAttribute(ctx, path.Root("cmk_spec"), &cmkSpec)

	if cluster.CMKSpec != nil {
		// Unmask the creds to store in the State file
		providerType := cluster.CMKSpec.ProviderType.Value
		switch providerType {
		case "AWS":
			cluster.CMKSpec.AWSCMKSpec.SecretKey.Value = cmkSpec.AWSCMKSpec.SecretKey.Value
			cluster.CMKSpec.AWSCMKSpec.AccessKey.Value = cmkSpec.AWSCMKSpec.AccessKey.Value
		case "GCP":
			cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.ClientId.Value = cmkSpec.GCPCMKSpec.GcpServiceAccount.ClientId.Value
			cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey.Value = cmkSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey.Value
		case "AZURE":
			cluster.CMKSpec.AzureCMKSpec.ClientSecret = types.String{Value: cmkSpec.AzureCMKSpec.ClientSecret.Value}
		}
	}

	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster", message)
		return
	}

	tflog.Debug(ctx, "Cluster Read: Allow List IDs read from API server", map[string]interface{}{
		"Allow List IDs": cluster.ClusterAllowListIDs})

	// set credentials for cluster (not returned by read api)
	req.State.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)
	// set restore backup id for cluster (not returned by read api)
	if !state.RestoreBackupID.Null {
		req.State.GetAttribute(ctx, path.Root("restore_backup_id"), &cluster.RestoreBackupID)
	}
	diags := setClusterState(ctx, &resp.State, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getClusterRegionIndex(region string, readOnly bool, regionIndexMap map[string]int, localIndex int) (index int) {
	if readOnly {
		return localIndex
	}
	index, ok := regionIndexMap[region]
	if ok {
		return index
	}
	return -1
}

func readBackupScheduleInfoV2(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string, scheduleId string) ([]BackupScheduleInfo, *http.Response, error) {
	backupScheduleResp, res, err := apiClient.BackupApi.GetBackupScheduleV2(ctx, accountId, projectId, clusterId, scheduleId).Execute()
	if err != nil {
		return nil, res, err
	}
	spec := backupScheduleResp.Data.GetSpec()
	backupScheduleInfo := make([]BackupScheduleInfo, 1)
	backupScheduleStruct := BackupScheduleInfo{

		State:                     types.String{Value: string(spec.GetState())},
		CronExpression:            types.String{Value: spec.GetCronExpression()},
		BackupDescription:         types.String{Value: spec.GetDescription()},
		RetentionPeriodInDays:     types.Int64{Value: int64(spec.GetRetentionPeriodInDays())},
		TimeIntervalInDays:        types.Int64{Value: int64(spec.GetTimeIntervalInDays())},
		IncrementalIntervalInMins: types.Int64{Value: int64(spec.GetIncrementalIntervalInMinutes())},
		ScheduleID:                types.String{Value: scheduleId},
	}
	if backupScheduleStruct.IncrementalIntervalInMins.Value == 0 {
		backupScheduleStruct.IncrementalIntervalInMins.Null = true
	}
	backupScheduleInfo[0] = backupScheduleStruct

	return backupScheduleInfo, nil, nil
}

func resourceClusterRead(ctx context.Context, accountId string, projectId string, clusterId string, backUpSchedules []BackupScheduleInfo, regions []string, allowListProvided bool, inputAllowListIDs []string, readOnly bool, apiClient *openapiclient.APIClient) (cluster Cluster, readOK bool, errorMessage string) {
	clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return cluster, false, errMsg
	}

	if len(backUpSchedules) > 0 {
		//Below if is used for handling empty array edge case
		if backUpSchedules[0].ScheduleID.Value == "" {
			backupScheduleInfo := make([]BackupScheduleInfo, 0)
			cluster.BackupSchedules = backupScheduleInfo
		}
		if backUpSchedules[0].ScheduleID.Value != "" {
			scheduleId := backUpSchedules[0].ScheduleID.Value
			var backupScheduleInfo []BackupScheduleInfo
			var res *http.Response
			var err error
			backupScheduleInfo, res, err = readBackupScheduleInfoV2(ctx, apiClient, accountId, projectId, clusterId, scheduleId)
			if err != nil {
				errMsg := getErrorMessage(res, err)
				return cluster, false, errMsg
			}
			cluster.BackupSchedules = backupScheduleInfo
		}
	}

	cmkResp, _, err := apiClient.ClusterApi.GetClusterCMK(context.Background(), accountId, projectId, clusterId).Execute()
	if cmkResp.Data != nil {
		cmkSpec := CMKSpec{}
		cmkDataSpec := cmkResp.GetData().Spec.Get()

		cmkSpec.ProviderType = types.String{Value: string(cmkDataSpec.GetProviderType())}
		cmkSpec.IsEnabled = types.Bool{Value: cmkDataSpec.GetIsEnabled()}

		switch cmkSpec.ProviderType.Value {
		case "AWS":
			awsCMKSpec := AWSCMKSpec{
				AccessKey: types.String{Value: cmkDataSpec.GetAwsCmkSpec().AccessKey},
				SecretKey: types.String{Value: cmkDataSpec.GetAwsCmkSpec().SecretKey},
				ARNList:   []types.String{},
			}
			cmkSpec.AWSCMKSpec = &awsCMKSpec

			for _, arn := range cmkDataSpec.GetAwsCmkSpec().ArnList {
				cmkSpec.AWSCMKSpec.ARNList = append(cmkSpec.AWSCMKSpec.ARNList, types.String{Value: arn})
			}
			cluster.CMKSpec = &cmkSpec

		case "GCP":
			gcpCMKSpec := GCPCMKSpec{
				KeyRingName:     types.String{Value: cmkDataSpec.GetGcpCmkSpec().KeyRingName},
				KeyName:         types.String{Value: cmkDataSpec.GetGcpCmkSpec().KeyName},
				Location:        types.String{Value: cmkDataSpec.GetGcpCmkSpec().Location},
				ProtectionLevel: types.String{Value: cmkDataSpec.GetGcpCmkSpec().ProtectionLevel},
			}

			if cmkDataSpec.GetGcpCmkSpec().GcpServiceAccount.IsSet() {
				gcpServiceAccountData := cmkDataSpec.GetGcpCmkSpec().GcpServiceAccount.Get()
				gcpServiceAccount := GCPServiceAccount{
					Type:                    types.String{Value: gcpServiceAccountData.Type},
					ProjectId:               types.String{Value: gcpServiceAccountData.ProjectId},
					PrivateKeyId:            types.String{Value: gcpServiceAccountData.PrivateKeyId},
					ClientEmail:             types.String{Value: gcpServiceAccountData.ClientEmail},
					ClientId:                types.String{Value: gcpServiceAccountData.ClientId},
					AuthUri:                 types.String{Value: gcpServiceAccountData.AuthUri},
					TokenUri:                types.String{Value: gcpServiceAccountData.TokenUri},
					AuthProviderX509CertUrl: types.String{Value: gcpServiceAccountData.AuthProviderX509CertUrl},
					ClientX509CertUrl:       types.String{Value: gcpServiceAccountData.ClientX509CertUrl},
				}
				if gcpServiceAccountData.GetPrivateKey() != "" {
					gcpServiceAccount.PrivateKey = types.String{Value: gcpServiceAccountData.GetPrivateKey()}
				}
				if gcpServiceAccountData.GetUniverseDomain() != "" {
					gcpServiceAccount.UniverseDomain = types.String{Value: gcpServiceAccountData.GetUniverseDomain()}
				} else {
					gcpServiceAccount.UniverseDomain.Null = true
				}
				gcpCMKSpec.GcpServiceAccount = gcpServiceAccount
			}

			cmkSpec.GCPCMKSpec = &gcpCMKSpec
			cluster.CMKSpec = &cmkSpec
		case "AZURE":
			azureCMKSpec := AzureCMKSpec{
				ClientID:     types.String{Value: cmkDataSpec.GetAzureCmkSpec().ClientId},
				ClientSecret: types.String{Value: cmkDataSpec.GetAzureCmkSpec().ClientSecret},
				TenantID:     types.String{Value: cmkDataSpec.GetAzureCmkSpec().TenantId},
				KeyVaultUri:  types.String{Value: cmkDataSpec.GetAzureCmkSpec().KeyVaultUri},
				KeyName:      types.String{Value: cmkDataSpec.GetAzureCmkSpec().KeyName},
			}

			cmkSpec.AzureCMKSpec = &azureCMKSpec
			cluster.CMKSpec = &cmkSpec
		}
	}

	// fill with all fields of schema except credentials - credentials are not returned by api call
	cluster.AccountID.Value = accountId
	cluster.ProjectID.Value = projectId
	cluster.ClusterID.Value = clusterId
	cluster.ClusterName.Value = clusterResp.Data.Spec.Name
	desiredState := "Active"
	if strings.EqualFold("Paused", string(clusterResp.Data.Info.GetState())) {
		desiredState = "Paused"
	}
	cluster.DesiredState.Value = desiredState
	if fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) {
		if clusterResp.Data.Info.GetIsConnectionPoolingEnabled() {
			cluster.DesiredConnectionPoolingState.Value = "Enabled"
		} else {
			cluster.DesiredConnectionPoolingState.Value = "Disabled"
		}
	}

	cluster.ClusterType.Value = string(*clusterResp.Data.Spec.ClusterInfo.ClusterType)
	cluster.ClusterTier.Value = string(clusterResp.Data.Spec.ClusterInfo.ClusterTier)
	cluster.ClusterVersion.Value = strconv.Itoa(int(clusterResp.Data.Spec.ClusterInfo.GetVersion()))

	// set database track name
	trackId := clusterResp.Data.Spec.SoftwareInfo.GetTrackId()
	trackName, trackNameOK, message := getTrackName(ctx, apiClient, accountId, trackId)
	if !trackNameOK {
		return cluster, false, message
	}
	cluster.DatabaseTrack.Value = trackName

	cluster.FaultTolerance.Value = string(clusterResp.Data.Spec.ClusterInfo.FaultTolerance)
	cluster.NumFaultsToTolerate.Value = int64(*clusterResp.Data.Spec.ClusterInfo.NumFaultsToTolerate.Get())
	nodeInfo := clusterResp.Data.Spec.ClusterInfo.NodeInfo.Get()
	if nodeInfo != nil {
		if cluster.NodeConfig == nil {
			cluster.NodeConfig = &NodeConfig{
				NumCores:   types.Int64{Value: int64((*nodeInfo).NumCores)},
				DiskSizeGb: types.Int64{Value: int64((*nodeInfo).DiskSizeGb)},
			}
		} else {
			cluster.NodeConfig.NumCores.Value = int64((*nodeInfo).NumCores)
			cluster.NodeConfig.DiskSizeGb.Value = int64((*nodeInfo).DiskSizeGb)
		}

		iopsPtr := (*nodeInfo).DiskIops.Get()
		if iopsPtr != nil {
			cluster.NodeConfig.DiskIops.Value = int64(*iopsPtr)
		}
	}

	cluster.ClusterInfo.State.Value = string(clusterResp.Data.Info.GetState())
	cluster.ClusterInfo.SoftwareVersion.Value = clusterResp.Data.Info.GetSoftwareVersion()
	cluster.ClusterInfo.CreatedTime.Value = clusterResp.Data.Info.Metadata.Get().GetCreatedOn()
	cluster.ClusterInfo.UpdatedTime.Value = clusterResp.Data.Info.Metadata.Get().GetUpdatedOn()

	// Cluster endpoints
	clusterEndpoints := types.Map{}
	clusterEndpoints.Elems = make(map[string]attr.Value)
	clusterEndpoints.ElemType = types.StringType
	for key, val := range clusterResp.Data.Info.Endpoints {
		clusterEndpoints.Elems[key] = types.String{Value: val}
	}
	cluster.ClusterEndpoints = clusterEndpoints

	// Cluster endpoints v2
	var clusterEndpointsV2 []ClusterEndpoint
	for _, val := range clusterResp.Data.Info.ClusterEndpoints {

		tflog.Debug(ctx, fmt.Sprintf("Cluster Endpoint %v %v %v", val.GetAccessibilityType(), val.GetHost(), val.Region))

		clusterEndpoint := ClusterEndpoint{
			AccessibilityType: types.String{Value: string(val.GetAccessibilityType())},
			Host:              types.String{Value: val.GetHost()},
			Region:            types.String{Value: val.Region},
		}
		clusterEndpointsV2 = append(clusterEndpointsV2, clusterEndpoint)
	}
	cluster.ClusterEndpointsV2 = clusterEndpointsV2

	// Cluster certificate
	certResponse, certHttpResp, err := apiClient.ClusterApi.GetConnectionCertificate(context.Background()).Execute()
	if err != nil {
		errMsg := getErrorMessage(certHttpResp, err)
		return cluster, false, errMsg
	}
	cluster.ClusterCertificate.Value = *certResponse.Data

	// This is being done to preserve order in the region list since an order mismatch is treated as state mismatch by Terraform
	regionIndexMap := map[string]int{}
	for index, region := range regions {
		regionIndexMap[region] = index
	}

	respClusterRegionInfo := clusterResp.Data.Spec.ClusterRegionInfo
	clusterRegionInfo := make([]RegionInfo, len(respClusterRegionInfo))
	for localIndex, info := range respClusterRegionInfo {
		region := info.PlacementInfo.CloudInfo.GetRegion()
		destIndex := getClusterRegionIndex(region, readOnly, regionIndexMap, localIndex)
		if destIndex < len(respClusterRegionInfo) {
			vpcID := info.PlacementInfo.GetVpcId()
			vpcName := ""
			if vpcID != "" {
				vpcData, err := getVPCByID(context.Background(), accountId, projectId, info.PlacementInfo.GetVpcId(), apiClient)
				if err != nil {
					return cluster, false, err.Error()
				}
				vpcName = vpcData.Spec.Name
			}

			// if info.AccessibilityTypes contains "PUBLIC" then set PublicAccess to true
			publicAccess := false
			for _, accessibilityType := range info.GetAccessibilityTypes() {
				if accessibilityType == "PUBLIC" {
					publicAccess = true
					break
				}
			}

			tflog.Debug(ctx, fmt.Sprintf("For region %v, publicAccess = %v", region, publicAccess))

			regionInfo := RegionInfo{
				Region:       types.String{Value: region},
				NumNodes:     types.Int64{Value: int64(info.PlacementInfo.GetNumNodes())},
				NumCores:     types.Int64{Value: int64(info.NodeInfo.Get().GetNumCores())},
				DiskSizeGb:   types.Int64{Value: int64(info.NodeInfo.Get().GetDiskSizeGb())},
				DiskIops:     types.Int64{Value: int64(info.NodeInfo.Get().GetDiskIops())},
				VPCID:        types.String{Value: vpcID},
				VPCName:      types.String{Value: vpcName},
				PublicAccess: types.Bool{Value: publicAccess},
				IsPreferred:  types.Bool{Value: info.GetIsAffinitized()},
				IsDefault:    types.Bool{Value: info.GetIsDefault()},
			}
			clusterRegionInfo[destIndex] = regionInfo
		}
	}
	cluster.ClusterRegionInfo = clusterRegionInfo
	cluster.CloudType.Value = string(respClusterRegionInfo[0].PlacementInfo.CloudInfo.GetCode())

	if allowListProvided {
		for {
			clusterAllowListMappingResp, response, err := apiClient.ClusterApi.ListClusterNetworkAllowLists(context.Background(), accountId, projectId, clusterId).Execute()
			if err != nil {
				errMsg := getErrorMessage(response, err)
				return cluster, false, errMsg
			}
			allowListIDMap := map[string]bool{}
			var allowListIDs []types.String
			var allowListStrings []string
			// This is being to done to preserve order in the list since an order mismatch is treated as state mismatch by Terraform
			for _, elem := range clusterAllowListMappingResp.Data {
				allowListIDMap[elem.Info.Id] = true
			}
			if !readOnly {
				for _, elem := range inputAllowListIDs {
					if _, ok := allowListIDMap[elem]; ok {
						allowListStrings = append(allowListStrings, elem)
					}
				}
			}
			if readOnly {
				for _, elem := range clusterAllowListMappingResp.Data {
					allowListStrings = append(allowListStrings, elem.Info.Id)
				}
			}
			tflog.Debug(context.Background(), fmt.Sprintf("Input Allow List is %v, Server Allow List is %v", inputAllowListIDs, allowListStrings))
			//added len(inputAllowListIDs)==0 in if condition so that we can reuse the func resourceClusterRead in data_source_cluster_name.go.
			if util.AreListsEqual(allowListStrings, inputAllowListIDs) || len(inputAllowListIDs) == 0 {
				for _, elem := range allowListStrings {
					allowListIDs = append(allowListIDs, types.String{Value: elem})
				}
				cluster.ClusterAllowListIDs = allowListIDs
				break
			}
			time.Sleep(1 * time.Second)
		}
	}

	return cluster, true, ""
}

func getClusterVersion(accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (version int, readOK bool, errorMessage string) {
	clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return 0, false, errMsg
	}

	return int(clusterResp.Data.Spec.ClusterInfo.GetVersion()), true, ""
}

func handleRestore(ctx context.Context, accountId string, projectId string, clusterId string, backupId string, apiClient *openapiclient.APIClient) error {
	restoreSpec := *openapiclient.NewRestoreSpec()
	restoreSpec.SetBackupId(backupId)
	restoreSpec.SetClusterId(clusterId)
	tflog.Debug(ctx, fmt.Sprintf("Restoring to cluster with cluster ID %v the backup with backup ID %v", clusterId, backupId))

	restoreResp, response, err := apiClient.BackupApi.RestoreBackup(ctx, accountId, projectId).RestoreSpec(restoreSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return errors.New("Unable to restore backup to cluster: " + errMsg)
	}

	restoreId := *restoreResp.Data.Info.Id
	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		restoreState, readInfoOK, message := getRestoreState(ctx, accountId, projectId, clusterId, backupId, restoreId, apiClient)
		if readInfoOK {
			if restoreState == "SUCCEEDED" {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get restore state: " + message))
		}
		return retry.RetryableError(errors.New("the backup restore is in progress"))
	})

	if err != nil {
		return errors.New("unable to restore backup to the cluster: The operation timed out waiting for backup restore")
	}

	return nil
}

// Update resource
func (r resourceCluster) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan Cluster
	resp.Diagnostics.Append(getPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.NodeConfig != nil && !plan.NodeConfig.DiskSizeGb.IsUnknown() && !util.IsDiskSizeValid(plan.ClusterTier.Value, plan.NodeConfig.DiskSizeGb.Value) {
		resp.Diagnostics.AddError("Invalid disk size", "The disk size for a paid cluster must be at least 50 GB.")
		return
	}

	if plan.NodeConfig != nil && !plan.NodeConfig.DiskIops.IsNull() && !plan.NodeConfig.DiskIops.IsUnknown() {
		isValid, err := util.IsDiskIopsValid(plan.CloudType.Value, plan.ClusterTier.Value, plan.NodeConfig.DiskIops.Value)
		if !isValid {
			resp.Diagnostics.AddError("Invalid disk IOPS", err)
			return
		}
	}

	apiClient := r.p.client
	var state Cluster
	getIDsFromState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value

	// Resume the cluster if the desired state is set to 'Active' and it is paused currently
	if strings.EqualFold(state.DesiredState.Value, "Paused") && (plan.DesiredState.Unknown || strings.EqualFold(plan.DesiredState.Value, "Active")) {
		// Resume the cluster
		tflog.Debug(ctx, "The cluster has been resumed successfully")
		err := resumeCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Cluster update failed: ", err.Error())
			return
		}
	}

	// Disable Connection Pooling if the desired state is set to 'Disabled' and it is enabled currently
	if fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) && !state.DesiredConnectionPoolingState.Unknown && strings.EqualFold(state.DesiredConnectionPoolingState.Value, "Enabled") && (plan.DesiredConnectionPoolingState.Unknown || strings.EqualFold(plan.DesiredConnectionPoolingState.Value, "Disabled")) {
		// Disable Connection Pooling
		tflog.Info(ctx, fmt.Sprintf("Existing Desired Connection Pooling State in State is %v", state.DesiredConnectionPoolingState.Value))
		err := disableConnectionPooling(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Disable connection pooling failed: ", err.Error())
			return
		}
	}

	for _, regionInfo := range plan.ClusterRegionInfo {
		vpcNamePresent := false
		vpcIDPresent := false
		if !regionInfo.VPCName.Unknown && !regionInfo.VPCName.Null && regionInfo.VPCName.Value != "" {
			vpcNamePresent = true
		}
		if !regionInfo.VPCID.Unknown && !regionInfo.VPCID.Null && regionInfo.VPCID.Value != "" {
			vpcIDPresent = true
		}
		if vpcNamePresent {
			if vpcIDPresent {
				resp.Diagnostics.AddError(
					"Specify VPC name or VPC ID",
					"To select a vpc, use either vpc_name or vpc_id. Don't provide both.",
				)
				return
			}
		}

		if !regionInfo.DiskSizeGb.IsUnknown() && !util.IsDiskSizeValid(plan.ClusterTier.Value, regionInfo.DiskSizeGb.Value) {
			resp.Diagnostics.AddError("Invalid disk size in "+regionInfo.Region.Value, "The disk size for a paid cluster must be at least 50 GB.")
			return
		}

		if !(regionInfo.DiskIops.IsUnknown() || regionInfo.DiskIops.IsNull()) {
			isValid, err := util.IsDiskIopsValid(plan.CloudType.Value, plan.ClusterTier.Value, regionInfo.DiskIops.Value)
			if !isValid {
				resp.Diagnostics.AddError("Invalid disk IOPS in "+regionInfo.Region.Value, err)
				return
			}
		}
	}

	scheduleId := ""
	backupDescription := ""
	var r1 *http.Response
	var err error

	scheduleId, backupDescription, r1, err = getBackupScheduleInfoV2(ctx, apiClient, accountId, projectId, clusterId)
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch the backup schedule for the cluster "+r1.Status, "Try again")
		return
	}

	clusterSpec, clusterOK, message := createClusterSpec(ctx, apiClient, accountId, projectId, plan, state, true)
	if !clusterOK {
		resp.Diagnostics.AddError("Unable to create cluster specification ", message)
		return
	}

	clusterVersion, versionOK, message := getClusterVersion(accountId, projectId, clusterId, apiClient)
	if !versionOK {
		resp.Diagnostics.AddError("Unable to get cluster version ", message)
		return
	}
	clusterSpec.ClusterInfo.SetVersion(int32(clusterVersion))
	_, response, err := apiClient.ClusterApi.EditCluster(context.Background(), accountId, projectId, clusterId).ClusterSpec(*clusterSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			resp.Diagnostics.AddError("Unable to edit cluster. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows: ",
				errMsg[:10000])
			return
		}
		resp.Diagnostics.AddError("Unable to edit cluster ", errMsg)
		return
	}

	// The following code has a pitfall:
	// If we change just the cluster_allow_list_ids field, then we will send a cluster edit
	// request to the server. The server will see the spec is the same as the current spec,
	// so there will be no task submitted.
	// If there is no task submitted (EVER), we will get a TASK_NOT_FOUND.
	// If there was EVER a task submitted, we will get the status of that task (likely SUCCESS).
	//
	// Challenges:
	// 1. Last EDIT was not successful - the customer should first perform an edit to get out of that state.
	// 2. To work around ANY possible race condition in the server side (task created AFTER the response),
	// we will try twice to read the task state. If both times we can't find the task, we bail out.
	//
	// Something similar will happen if changing the backup schedule or the CMK spec.
	retries := 0
	readClusterRetries := 0
	checkNewTaskSpawned := true
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_EDIT_CLUSTER, apiClient, ctx)

		tflog.Info(ctx, "Cluster edit operation in progress, state: "+asState)

		if readInfoOK {
			if asState == "TASK_NOT_FOUND" {
				// We try for a minute waiting for the tasks to be spawned. If edit cluster responded with a success
				// without creating a task for about a minute, we can safely assume that a task is not required to be spawned.
				// We also test for the cluster to be in an active state in that code that follows. So, we can safely do this.
				if retries < 6 {
					retries++
					tflog.Info(ctx, "Cluster edit task not found, retrying...")
					return retry.RetryableError(errors.New("cluster not found, retrying"))
				} else {
					tflog.Info(ctx, "Cluster edit task not found, the change would not have required a task creation")
					return nil
				}
			}
			// There are cases this code flow checks for the state of previously spawned tasks instead of checking for new tasks.
			// Hence, we check whether a new task is spawned.
			if checkNewTaskSpawned {
				if asState == string(openapiclient.TASKACTIONSTATEENUM_IN_PROGRESS) {
					checkNewTaskSpawned = false
					return retry.RetryableError(errors.New("cluster edit operation in progress"))
				} else {
					tflog.Info(ctx, "Cluster edit task not found, the change would not have required a task creation")
					return nil
				}
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}

		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("cluster edit operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for cluster edit to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "cluster edit operation failed"
		}
		resp.Diagnostics.AddError("Unable to edit cluster:", msg)
		return
	}

	// read status, wait for status to be active
	readClusterRetries = 0
	retryPolicyA := retry.NewConstant(10 * time.Second)
	retryPolicyA = retry.WithMaxDuration(3600*time.Second, retryPolicyA)
	err = retry.Do(ctx, retryPolicyA, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") || clusterState == "Create Failed" || clusterState == "CREATE_FAILED" {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("cluster edit is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to edit cluster:", "The operation timed out waiting for cluster edit to complete.")
		return
	}

	var backUpSchedules []BackupScheduleInfo
	if plan.BackupSchedules != nil && len(plan.BackupSchedules) > 0 {
		if len(plan.BackupSchedules) > 1 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "More than one schedules were passed")
			return
		}
		if plan.BackupSchedules[0].State.Value != "" && plan.BackupSchedules[0].RetentionPeriodInDays.Value == 0 {
			resp.Diagnostics.AddError("Unable to modify backup schedule", "You must provide both state and retention period in days.")
			return
		}
		if plan.BackupSchedules[0].State.Value == "" && plan.BackupSchedules[0].RetentionPeriodInDays.Value != 0 {
			resp.Diagnostics.AddError("Unable to modify backup schedule", "You must provide both state and retention period in days.")
			return
		}
		tflog.Info(ctx, fmt.Sprintf("User defined description '%v' default description '%v'", plan.BackupSchedules[0].BackupDescription.Value, backupDescription))
		newDescription := ""

		if plan.BackupSchedules[0].BackupDescription.Value == "" {
			newDescription = backupDescription
		} else {
			newDescription = plan.BackupSchedules[0].BackupDescription.Value
		}
		err = EditBackupSchedule(ctx, plan.BackupSchedules[0], scheduleId, newDescription, accountId, projectId, clusterId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error duing store: ", err.Error())
			return
		}

		backupScheduleStruct := BackupScheduleInfo{
			ScheduleID: types.String{Value: scheduleId},
		}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)
	}

	if plan.BackupSchedules != nil && len(plan.BackupSchedules) == 0 {
		backupScheduleStruct := BackupScheduleInfo{}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)

	}

	if plan.CMKSpec != nil {
		cmkSpec, err := createCmkSpec(plan)
		if err != nil {
			resp.Diagnostics.AddError("Error creating CMK Spec: ", err.Error())
			return
		} else {
			err = editClusterCmk(ctx, apiClient, accountId, projectId, clusterId, *cmkSpec)
			if err != nil {
				resp.Diagnostics.AddError("Cluster CMK update failed: ", err.Error())
				return
			}
		}
	}

	var allowListIDs []string
	allowListProvided := false

	if plan.ClusterAllowListIDs != nil {
		for i := range plan.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, plan.ClusterAllowListIDs[i].Value)
		}

		_, response, err := apiClient.ClusterApi.EditClusterNetworkAllowLists(context.Background(), accountId, projectId, clusterId).RequestBody(allowListIDs).Execute()
		if err != nil {
			errMsg := getErrorMessage(response, err)
			resp.Diagnostics.AddError("Unable to assign allow list to cluster ", errMsg)
			return
		}
		allowListProvided = true
	}

	tflog.Debug(ctx, "Cluster Update: Details about allow list IDs", map[string]interface{}{
		"Allow List IDs":  allowListIDs,
		"Provided or Not": allowListProvided})

	restoreRequired := false
	backupId := ""
	if !plan.RestoreBackupID.Unknown && !plan.RestoreBackupID.Null && plan.RestoreBackupID.Value != "" {
		if state.RestoreBackupID.Value != plan.RestoreBackupID.Value {
			restoreRequired = true
		}
		backupId = plan.RestoreBackupID.Value
	}
	if restoreRequired {
		err = handleRestore(ctx, accountId, projectId, clusterId, backupId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error during store: ", err.Error())
			return
		}
	}

	// Pause the cluster if the desired state is set to 'Paused' and it is active currently
	if strings.EqualFold(state.DesiredState.Value, "Active") && (!plan.DesiredState.Unknown && strings.EqualFold(plan.DesiredState.Value, "Paused")) {
		// Pause the cluster
		err := pauseCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Cluster update failed: ", err.Error())
			return
		}
	}

	// Enable connection pooling if the desired state is set to 'Enabled'
	if fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) && !plan.DesiredConnectionPoolingState.Unknown && strings.EqualFold(plan.DesiredConnectionPoolingState.Value, "Enabled") {
		err := enableConnectionPooling(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Enabling connection pooling Failed: ", err.Error())
		}
	}

	var regions []string
	for _, regionInfo := range plan.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.Value)
	}

	cluster, readOK, message := resourceClusterRead(ctx, accountId, projectId, clusterId, backUpSchedules, regions, allowListProvided, allowListIDs, false, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster ", message)
		return
	}
	tflog.Debug(ctx, "Cluster Update: Allow list IDs read from API server ", map[string]interface{}{
		"Allow List IDs": cluster.ClusterAllowListIDs})

	// Update the State file with the unmasked creds for AWS (Secret Key, Access Key), GCP (Client ID, Private Key)
	// and Azure (client ID, client Secret, tenant ID)
	if plan.CMKSpec != nil {
		providerType := cluster.CMKSpec.ProviderType.Value
		switch providerType {
		case "AWS":
			cluster.CMKSpec.AWSCMKSpec.SecretKey = plan.CMKSpec.AWSCMKSpec.SecretKey
			cluster.CMKSpec.AWSCMKSpec.AccessKey = plan.CMKSpec.AWSCMKSpec.AccessKey
		case "GCP":
			cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.ClientId = plan.CMKSpec.GCPCMKSpec.GcpServiceAccount.ClientId
			cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey = plan.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey
		case "AZURE":
			cluster.CMKSpec.AzureCMKSpec.ClientSecret = plan.CMKSpec.AzureCMKSpec.ClientSecret
		}
	}

	// set credentials for cluster (not returned by read api)
	req.State.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)
	// set restore backup id for cluster (not returned by read api)
	if restoreRequired {
		cluster.RestoreBackupID.Value = plan.RestoreBackupID.Value
	} else {
		cluster.RestoreBackupID.Null = true
	}

	if fflags.IsFeatureFlagEnabled(fflags.CONNECTION_POOLING) && (strings.EqualFold(plan.DesiredConnectionPoolingState.Value, "Enabled") || strings.EqualFold(plan.DesiredConnectionPoolingState.Value, "Disabled")) {
		cluster.DesiredConnectionPoolingState.Value = plan.DesiredConnectionPoolingState.Value
	}
	diags := setClusterState(ctx, &resp.State, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete resource
func (r resourceCluster) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state Cluster
	getIDsFromState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value

	apiClient := r.p.client

	_, err := apiClient.ClusterApi.DeleteCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete the cluster ", GetApiErrorDetails(err))
		return
	}

	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_DELETE_CLUSTER, apiClient, ctx)

		tflog.Info(ctx, "Cluster delete operation in progress, state: "+asState)

		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("cluster deletion operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for cluster deletion to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "cluster deletion operation failed"
		}
		resp.Diagnostics.AddError("Unable to delete cluster:", msg)
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState Import resource
func (r resourceCluster) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
