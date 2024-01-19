/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type Cluster struct {
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
	NodeConfig          NodeConfig           `tfsdk:"node_config"`
	Credentials         Credentials          `tfsdk:"credentials"`
	ClusterInfo         ClusterInfo          `tfsdk:"cluster_info"`
	ClusterVersion      types.String         `tfsdk:"cluster_version"`
	BackupSchedules     []BackupScheduleInfo `tfsdk:"backup_schedules"`
	ClusterEndpoints    types.Map            `tfsdk:"cluster_endpoints"`
	ClusterEndpointsV2  []ClusterEndpoint    `tfsdk:"endpoints"`
	ClusterCertificate  types.String         `tfsdk:"cluster_certificate"`
	CMKSpec             *CMKSpec             `tfsdk:"cmk_spec"`
}

type ClusterEndpoint struct {
	AccessibilityType types.String `tfsdk:"accessibility_type"`
	Host              types.String `tfsdk:"host"`
	Region            types.String `tfsdk:"region"`
}

type CMKSpec struct {
	ProviderType types.String  `tfsdk:"provider_type"`
	AWSCMKSpec   *AWSCMKSpec   `tfsdk:"aws_cmk_spec"`
	GCPCMKSpec   *GCPCMKSpec   `tfsdk:"gcp_cmk_spec"`
	AzureCMKSpec *AzureCMKSpec `tfsdk:"azure_cmk_spec"`
	IsEnabled    types.Bool    `tfsdk:"is_enabled"`
}

type AWSCMKSpec struct {
	AccessKey types.String   `tfsdk:"access_key"`
	SecretKey types.String   `tfsdk:"secret_key"`
	ARNList   []types.String `tfsdk:"arn_list"`
}

type GCPCMKSpec struct {
	KeyRingName       types.String      `tfsdk:"key_ring_name"`
	KeyName           types.String      `tfsdk:"key_name"`
	Location          types.String      `tfsdk:"location"`
	ProtectionLevel   types.String      `tfsdk:"protection_level"`
	GcpServiceAccount GCPServiceAccount `tfsdk:"gcp_service_account"`
}

type AzureCMKSpec struct {
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	TenantID     types.String `tfsdk:"tenant_id"`
	KeyVaultUri  types.String `tfsdk:"key_vault_uri"`
	KeyName      types.String `tfsdk:"key_name"`
}

type GCPServiceAccount struct {
	Type                    types.String `tfsdk:"type"`
	ProjectId               types.String `tfsdk:"project_id"`
	PrivateKey              types.String `tfsdk:"private_key"`
	PrivateKeyId            types.String `tfsdk:"private_key_id"`
	ClientEmail             types.String `tfsdk:"client_email"`
	ClientId                types.String `tfsdk:"client_id"`
	AuthUri                 types.String `tfsdk:"auth_uri"`
	TokenUri                types.String `tfsdk:"token_uri"`
	AuthProviderX509CertUrl types.String `tfsdk:"auth_provider_x509_cert_url"`
	ClientX509CertUrl       types.String `tfsdk:"client_x509_cert_url"`
	UniverseDomain          types.String `tfsdk:"universe_domain"`
}

type BackupScheduleInfo struct {
	State                 types.String `tfsdk:"state"`
	RetentionPeriodInDays types.Int64  `tfsdk:"retention_period_in_days"`
	ScheduleID            types.String `tfsdk:"schedule_id"`
	BackupDescription     types.String `tfsdk:"backup_description"`
	CronExpression        types.String `tfsdk:"cron_expression"`
	TimeIntervalInDays    types.Int64  `tfsdk:"time_interval_in_days"`
}
type RegionInfo struct {
	Region       types.String `tfsdk:"region"`
	NumNodes     types.Int64  `tfsdk:"num_nodes"`
	VPCID        types.String `tfsdk:"vpc_id"`
	VPCName      types.String `tfsdk:"vpc_name"`
	PublicAccess types.Bool   `tfsdk:"public_access"`
}

type NodeConfig struct {
	NumCores   types.Int64 `tfsdk:"num_cores"`
	DiskSizeGb types.Int64 `tfsdk:"disk_size_gb"`
	DiskIops   types.Int64 `tfsdk:"disk_iops"`
}

type Credentials struct {
	Username     types.String `tfsdk:"username"`
	Password     types.String `tfsdk:"password"`
	YSQLUsername types.String `tfsdk:"ysql_username"`
	YSQLPassword types.String `tfsdk:"ysql_password"`
	YCQLUsername types.String `tfsdk:"ycql_username"`
	YCQLPassword types.String `tfsdk:"ycql_password"`
}

type ClusterInfo struct {
	State           types.String `tfsdk:"state"`
	SoftwareVersion types.String `tfsdk:"software_version"`
	CreatedTime     types.String `tfsdk:"created_time"`
	UpdatedTime     types.String `tfsdk:"updated_time"`
}

type AllowList struct {
	AccountID            types.String   `tfsdk:"account_id"`
	ProjectID            types.String   `tfsdk:"project_id"`
	AllowListID          types.String   `tfsdk:"allow_list_id"`
	AllowListName        types.String   `tfsdk:"allow_list_name"`
	AllowListDescription types.String   `tfsdk:"allow_list_description"`
	CIDRList             []types.String `tfsdk:"cidr_list"`
	ClusterIDs           []types.String `tfsdk:"cluster_ids"`
}

type Backup struct {
	AccountID             types.String `tfsdk:"account_id"`
	ProjectID             types.String `tfsdk:"project_id"`
	ClusterID             types.String `tfsdk:"cluster_id"`
	BackupID              types.String `tfsdk:"backup_id"`
	BackupDescription     types.String `tfsdk:"backup_description"`
	RetentionPeriodInDays types.Int64  `tfsdk:"retention_period_in_days"`
	MostRecent            types.Bool   `tfsdk:"most_recent"`
	Timestamp             types.String `tfsdk:"timestamp"`
}

type VPC struct {
	AccountID      types.String    `tfsdk:"account_id"`
	ProjectID      types.String    `tfsdk:"project_id"`
	VPCID          types.String    `tfsdk:"vpc_id"`
	Name           types.String    `tfsdk:"name"`
	Cloud          types.String    `tfsdk:"cloud"`
	GlobalCIDR     types.String    `tfsdk:"global_cidr"`
	ExternalVPCID  types.String    `tfsdk:"external_vpc_id"`
	RegionCIDRInfo []VPCRegionInfo `tfsdk:"region_cidr_info"`
}

type VPCRegionInfo struct {
	Region types.String `tfsdk:"region"`
	CIDR   types.String `tfsdk:"cidr"`
}

type ReadReplicas struct {
	AccountID        types.String      `tfsdk:"account_id"`
	ProjectID        types.String      `tfsdk:"project_id"`
	ReadReplicasInfo []ReadReplicaInfo `tfsdk:"read_replicas_info"`
	PrimaryClusterID types.String      `tfsdk:"primary_cluster_id"`
}

type ReadReplicaInfo struct {
	CloudType   types.String `tfsdk:"cloud_type"`
	NumNodes    types.Int64  `tfsdk:"num_nodes"`
	NumReplicas types.Int64  `tfsdk:"num_replicas"`
	Region      types.String `tfsdk:"region"`
	VPCID       types.String `tfsdk:"vpc_id"`
	VPCName     types.String `tfsdk:"vpc_name"`
	NodeConfig  NodeConfig   `tfsdk:"node_config"`
	MultiZone   types.Bool   `tfsdk:"multi_zone"`
	Endpoint    types.String `tfsdk:"endpoint"`
}

type VPCPeering struct {
	AccountID          types.String       `tfsdk:"account_id"`
	ProjectID          types.String       `tfsdk:"project_id"`
	Name               types.String       `tfsdk:"name"`
	VPCPeeringID       types.String       `tfsdk:"vpc_peering_id"`
	YugabyteDBVPCID    types.String       `tfsdk:"yugabytedb_vpc_id"`
	ApplicationVPCInfo ApplicationVPCInfo `tfsdk:"application_vpc_info"`
	VPCPeeringState    types.String       `tfsdk:"vpc_peering_state"`
}

type ApplicationVPCInfo struct {
	Cloud     types.String `tfsdk:"cloud"`
	Project   types.String `tfsdk:"project"`
	AccountID types.String `tfsdk:"account_id"`
	Region    types.String `tfsdk:"region"`
	VPCID     types.String `tfsdk:"vpc_id"`
	CIDR      types.String `tfsdk:"cidr"`
}

type User struct {
	AccountID types.String `tfsdk:"account_id"`
	ProjectID types.String `tfsdk:"project_id"`
	Email     types.String `tfsdk:"email"`
	RoleName  types.String `tfsdk:"role_name"`
	UserName  types.String `tfsdk:"user_name"`
	UserState types.String `tfsdk:"user_state"`
	UserID    types.String `tfsdk:"user_id"`
}

type Role struct {
	AccountID               types.String     `tfsdk:"account_id"`
	ProjectID               types.String     `tfsdk:"project_id"`
	RoleID                  types.String     `tfsdk:"role_id"`
	RoleName                types.String     `tfsdk:"role_name"`
	RoleDescription         types.String     `tfsdk:"role_description"`
	PermissionList          []PermissionInfo `tfsdk:"permission_list"`
	EffectivePermissionList []PermissionInfo `tfsdk:"effective_permission_list"`
	Users                   []types.String   `tfsdk:"users"`
	ApiKeys                 []types.String   `tfsdk:"api_keys"`
}

type PermissionInfo struct {
	ResourceType    types.String   `tfsdk:"resource_type"`
	OperationGroups []types.String `tfsdk:"operation_groups"`
}

type PrivateServiceEndpoint struct {
	AccountID                types.String   `tfsdk:"account_id"`
	ProjectID                types.String   `tfsdk:"project_id"`
	ClusterID                types.String   `tfsdk:"cluster_id"`
	PrivateServiceEndpointID types.String   `tfsdk:"endpoint_id"`
	AvailabilityZones        []types.String `tfsdk:"availability_zones"`
	ServiceName              types.String   `tfsdk:"service_name"`
	ClusterRegionInfoId      types.String   `tfsdk:"cluster_region_info_id"`
	Region                   types.String   `tfsdk:"region"`
	SecurityPrincipals       []types.String `tfsdk:"security_principals"`
	Host                     types.String   `tfsdk:"host"`
	State                    types.String   `tfsdk:"state"`
}

type ApiKey struct {
	AccountID   types.String `tfsdk:"account_id"`
	ProjectID   types.String `tfsdk:"project_id"`
	Name        types.String `tfsdk:"name"`
	ApiKey      types.String `tfsdk:"api_key"`
	Duration    types.Int64  `tfsdk:"duration"`
	Unit        types.String `tfsdk:"unit"`
	Description types.String `tfsdk:"description"`
	RoleName    types.String `tfsdk:"role_name"`
	Status      types.String `tfsdk:"status"`
	ApiKeyID    types.String `tfsdk:"api_key_id"`
	Issuer      types.String `tfsdk:"issuer"`
	LastUsed    types.String `tfsdk:"last_used"`
	ExpiryTime  types.String `tfsdk:"expiration"`
	CreatedAt   types.String `tfsdk:"date_created"`
}

type MetricsExporter struct {
	AccountID     types.String   `tfsdk:"account_id"`
	ProjectID     types.String   `tfsdk:"project_id"`
	ConfigID      types.String   `tfsdk:"config_id"`
	ConfigName    types.String   `tfsdk:"config_name"`
	Type          types.String   `tfsdk:"type"`
	DataDogSpec   *DataDogSpec   `tfsdk:"datadog_spec"`
	GrafanaSpec   *GrafanaSpec   `tfsdk:"grafana_spec"`
	SumoLogicSpec *SumoLogicSpec `tfsdk:"sumologic_spec"`
}

type DataDogSpec struct {
	Site   types.String `tfsdk:"site"`
	ApiKey types.String `tfsdk:"api_key"`
}

type GrafanaSpec struct {
	AccessTokenPolicy types.String `tfsdk:"access_policy_token"`
	Zone              types.String `tfsdk:"zone"`
	InstanceId        types.String `tfsdk:"instance_id"`
	OrgSlug           types.String `tfsdk:"org_slug"`
}

type SumoLogicSpec struct {
	AccessKey         types.String `tfsdk:"access_key"`
	AccessId          types.String `tfsdk:"access_id"`
	InstallationToken types.String `tfsdk:"installation_token"`
}

func (d DataDogSpec) EncryptedKey() string {
	return obfuscateString(d.ApiKey.Value)
}

func (g GrafanaSpec) EncryptedKey() string {
	return obfuscateStringLenght(g.AccessTokenPolicy.Value, 5)
}

func (s SumoLogicSpec) EncryptedKey(key string) string {
	switch key {

	case "access_key":
		return obfuscateString(s.AccessKey.Value)
	case "access_id":
		return obfuscateString(s.AccessId.Value)
	case "installation_token":
		return obfuscateString(s.InstallationToken.Value)
	}
	return ""
}

type AssociateMetricsExporterCluster struct {
	AccountID   types.String `tfsdk:"account_id"`
	ProjectID   types.String `tfsdk:"project_id"`
	ConfigID    types.String `tfsdk:"config_id"`
	ConfigName  types.String `tfsdk:"config_name"`
	ClusterID   types.String `tfsdk:"cluster_id"`
	ClusterName types.String `tfsdk:"cluster_name"`
}
