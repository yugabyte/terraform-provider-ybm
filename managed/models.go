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
	ClusterRegionInfo   []RegionInfo         `tfsdk:"cluster_region_info"`
	ClusterTier         types.String         `tfsdk:"cluster_tier"`
	ClusterAllowListIDs []types.String       `tfsdk:"cluster_allow_list_ids"`
	RestoreBackupID     types.String         `tfsdk:"restore_backup_id"`
	NodeConfig          NodeConfig           `tfsdk:"node_config"`
	Credentials         Credentials          `tfsdk:"credentials"`
	ClusterInfo         ClusterInfo          `tfsdk:"cluster_info"`
	ClusterVersion      types.String         `tfsdk:"cluster_version"`
	BackupSchedules     []BackupScheduleInfo `tfsdk:"backup_schedules"`
	ClusterEndpoints    types.Map            `tfsdk:"cluster_endpoints"`
	ClusterCertificate  types.String         `tfsdk:"cluster_certificate"`
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
	Region   types.String `tfsdk:"region"`
	NumNodes types.Int64  `tfsdk:"num_nodes"`
	VPCID    types.String `tfsdk:"vpc_id"`
}

type NodeConfig struct {
	NumCores   types.Int64 `tfsdk:"num_cores"`
	MemoryMb   types.Int64 `tfsdk:"memory_mb"`
	DiskSizeGb types.Int64 `tfsdk:"disk_size_gb"`
}

type Credentials struct {
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
	ReadReplicaID types.String `tfsdk:"read_replica_id"`
	CloudType     types.String `tfsdk:"cloud_type"`
	NumNodes      types.Int64  `tfsdk:"num_nodes"`
	NumReplicas   types.Int64  `tfsdk:"num_replicas"`
	Region        types.String `tfsdk:"region"`
	VPCID         types.String `tfsdk:"vpc_id"`
	NodeConfig    NodeConfig   `tfsdk:"node_config"`
	Endpoint      types.String `tfsdk:"endpoint"`
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
	Cloud   types.String `tfsdk:"cloud"`
	Project types.String `tfsdk:"project"`
	Region  types.String `tfsdk:"region"`
	VPCID   types.String `tfsdk:"vpc_id"`
	CIDR    types.String `tfsdk:"cidr"`
}
