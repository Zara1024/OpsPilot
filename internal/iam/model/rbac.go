package model

import "time"

// 资源级 RBAC 主体类型枚举。
const (
	SubjectTypeUser = "user"
	SubjectTypeOrg  = "org"
)

// IsValidSubjectType 校验授权主体类型。
func IsValidSubjectType(t string) bool {
	return t == SubjectTypeUser || t == SubjectTypeOrg
}

// 资源级 RBAC 资源类别枚举。
const (
	RBACResourceDevice     = "device"
	RBACResourceDeviceEnv  = "device_env"
	RBACResourceK8sCluster = "k8s_cluster"
	RBACResourceService    = "service"
)

// IsValidRBACResourceType 校验资源类型。
func IsValidRBACResourceType(t string) bool {
	switch t {
	case RBACResourceDevice, RBACResourceDeviceEnv, RBACResourceK8sCluster, RBACResourceService:
		return true
	default:
		return false
	}
}

// 资源级 RBAC 动作及规则效力枚举。
const (
	RBACActionRead   = "read"
	RBACActionWrite  = "write"
	RBACActionExec   = "exec"
	RBACActionManage = "manage"
	RBACActionAll    = "*"

	RBACEffectAllow = "allow"
	RBACEffectDeny  = "deny"
)

// IsValidRBACAction 校验单个动作标识是否合法。
func IsValidRBACAction(a string) bool {
	switch a {
	case RBACActionRead, RBACActionWrite, RBACActionExec, RBACActionManage, RBACActionAll:
		return true
	default:
		return false
	}
}

// IsValidRBACEffect 校验规则效力。
func IsValidRBACEffect(e string) bool {
	return e == RBACEffectAllow || e == RBACEffectDeny
}

// ResourceGrant 表示绑定到用户或组织的资源级权限策略（显式 Allow / Deny）。
type ResourceGrant struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement"`
	SubjectType     string    `gorm:"type:varchar(16);not null;index:idx_rbac_subj,priority:1"`
	SubjectID       uint64    `gorm:"not null;index:idx_rbac_subj,priority:2"`
	ResourceType    string    `gorm:"type:varchar(32);not null;index"`
	ResourcePattern string    `gorm:"type:varchar(255);not null"`
	Actions         string    `gorm:"type:varchar(128);not null"`
	Effect          string    `gorm:"type:varchar(16);not null;index"`
	Description     string    `gorm:"type:varchar(255)"`
	CreatedBy       uint64    `gorm:"not null;default:0"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (ResourceGrant) TableName() string { return "iam_resource_grants" }

// ResourceGrantFilter 定义查询资源授权规则的可选过滤条件。
type ResourceGrantFilter struct {
	SubjectType  string
	SubjectID    uint64
	ResourceType string
	Effect       string
}
