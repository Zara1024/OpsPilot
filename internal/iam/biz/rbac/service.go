// Package rbac 实现资源级访问控制策略管理与实时鉴权判定（显式 Deny 优先，存在 Allow 规则时启用白名单隔离，否则兼容现有组织角色）。
package rbac

import (
	"context"
	"fmt"
	"strings"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// ListFilter 定义查询授权规则的过滤参数。
type ListFilter = model.ResourceGrantFilter

// Store 定义资源授权规则持久化契约。
type Store interface {
	List(ctx context.Context, f model.ResourceGrantFilter) ([]*model.ResourceGrant, error)
	ListForSubjects(ctx context.Context, userID uint64, orgIDs []uint64, resourceType string) ([]*model.ResourceGrant, error)
	GetByID(ctx context.Context, id uint64) (*model.ResourceGrant, error)
	Create(ctx context.Context, g *model.ResourceGrant) error
	Update(ctx context.Context, g *model.ResourceGrant) error
	Delete(ctx context.Context, id uint64) error
}

// MembershipReader 查询用户所属组织以支持组织级授权继承。
type MembershipReader interface {
	ListByUser(ctx context.Context, userID uint64) ([]model.MembershipWithOrg, error)
}

// UserRoleReader 查询模拟校验目标用户的系统角色，可选。
type UserRoleReader interface {
	GetByID(ctx context.Context, id uint64) (*model.User, error)
}

// GrantInput 为创建/更新资源授权规则的入参。
type GrantInput struct {
	SubjectType     string `json:"subject_type"`
	SubjectID       uint64 `json:"subject_id"`
	ResourceType    string `json:"resource_type"`
	ResourcePattern string `json:"resource_pattern"`
	Actions         string `json:"actions"`
	Effect          string `json:"effect"`
	Description     string `json:"description"`
}

// Decision 描述一次资源鉴权判定的详细结果（可用于审计及管理员权限模拟器）。
type Decision struct {
	Allowed        bool    `json:"allowed"`
	Reason         string  `json:"reason"`
	MatchedGrantID *uint64 `json:"matched_grant_id,omitempty"`
	MatchedRule    string  `json:"matched_rule,omitempty"`
}

// Service 提供资源授权规则管理与 CheckResource/AllowResource 判定引擎。
type Service struct {
	store       Store
	memberships MembershipReader
	users       UserRoleReader
}

// New 构造 RBAC Service。
func New(store Store, memberships MembershipReader, users UserRoleReader) *Service {
	return &Service{
		store:       store,
		memberships: memberships,
		users:       users,
	}
}

// ListGrants 按条件列出授权规则。
func (s *Service) ListGrants(ctx context.Context, f model.ResourceGrantFilter) ([]*model.ResourceGrant, error) {
	if s.store == nil {
		return nil, errs.ErrNotWiredYet
	}
	return s.store.List(ctx, f)
}

// CreateGrant 校验并新增一条资源级授权/拒绝规则。
func (s *Service) CreateGrant(ctx context.Context, actorID uint64, in GrantInput) (*model.ResourceGrant, error) {
	g := &model.ResourceGrant{CreatedBy: actorID}
	if err := normalizeAndValidateGrant(g, in); err != nil {
		return nil, err
	}
	if err := s.store.Create(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// UpdateGrant 更新已有授权规则。
func (s *Service) UpdateGrant(ctx context.Context, id uint64, in GrantInput) (*model.ResourceGrant, error) {
	g, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := normalizeAndValidateGrant(g, in); err != nil {
		return nil, err
	}
	if err := s.store.Update(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

// DeleteGrant 删除指定授权规则。
func (s *Service) DeleteGrant(ctx context.Context, id uint64) error {
	return s.store.Delete(ctx, id)
}

// AllowResource 满足 pkg/authzmw.ResourceAuthorizer 接口。
func (s *Service) AllowResource(ctx context.Context, userID uint64, isAdmin bool, resourceType string, resourceKeys []string, action string) (bool, error) {
	dec, err := s.Check(ctx, userID, isAdmin, resourceType, resourceKeys, action)
	if err != nil {
		return false, err
	}
	return dec.Allowed, nil
}

// Check 执行完整的资源级鉴权决策并返回原因与命中的规则信息。
func (s *Service) Check(ctx context.Context, userID uint64, isAdmin bool, resourceType string, resourceKeys []string, action string) (Decision, error) {
	if isAdmin {
		return Decision{Allowed: true, Reason: "admin_bypass"}, nil
	}
	// 当通过模拟器仅传入 userID 时（isAdmin=false），如配置了 users reader 则核查其实际系统角色。
	if !isAdmin && s.users != nil && userID > 0 {
		if u, err := s.users.GetByID(ctx, userID); err == nil && u != nil && (u.Role == model.RoleAdmin || u.IsSuperuser) {
			return Decision{Allowed: true, Reason: "admin_bypass"}, nil
		}
	}
	rt := strings.ToLower(strings.TrimSpace(resourceType))
	act := strings.ToLower(strings.TrimSpace(action))
	if !model.IsValidRBACResourceType(rt) || !model.IsValidRBACAction(act) {
		return Decision{Allowed: false, Reason: "invalid_resource_or_action"}, fmt.Errorf("%w: invalid resource_type=%q action=%q", errs.ErrInvalid, resourceType, action)
	}

	var orgIDs []uint64
	if s.memberships != nil && userID > 0 {
		ms, err := s.memberships.ListByUser(ctx, userID)
		if err != nil {
			return Decision{Allowed: false, Reason: "membership_lookup_error"}, err
		}
		for _, m := range ms {
			orgIDs = append(orgIDs, m.OrgID)
		}
	}

	grants, err := s.store.ListForSubjects(ctx, userID, orgIDs, rt)
	if err != nil {
		return Decision{Allowed: false, Reason: "grant_lookup_error"}, err
	}

	cleanKeys := make([]string, 0, len(resourceKeys))
	for _, k := range resourceKeys {
		if t := strings.TrimSpace(k); t != "" {
			cleanKeys = append(cleanKeys, t)
		}
	}

	// 1. 显式 Deny 最高优先级。
	for _, g := range grants {
		if g.Effect != model.RBACEffectDeny {
			continue
		}
		if actionsCover(g.Actions, act) && matchesAnyKey(g.ResourcePattern, cleanKeys) {
			id := g.ID
			return Decision{
				Allowed:        false,
				Reason:         "explicit_deny",
				MatchedGrantID: &id,
				MatchedRule:    formatRuleSummary(g),
			}, nil
		}
	}

	// 2. 收集该主体对该资源类型的全部 Allow 规则：一旦存在至少一条 Allow 规则即激活白名单隔离。
	hasAllowWhitelist := false
	for _, g := range grants {
		if g.Effect != model.RBACEffectAllow {
			continue
		}
		hasAllowWhitelist = true
		if actionsCover(g.Actions, act) && matchesAnyKey(g.ResourcePattern, cleanKeys) {
			id := g.ID
			return Decision{
				Allowed:        true,
				Reason:         "explicit_allow",
				MatchedGrantID: &id,
				MatchedRule:    formatRuleSummary(g),
			}, nil
		}
	}

	if hasAllowWhitelist {
		return Decision{Allowed: false, Reason: "not_in_allow_scope"}, nil
	}

	// 3. 无 Allow 白名单且未命中 Deny，沿用现有组织角色/Casbin 默认策略。
	return Decision{Allowed: true, Reason: "default_org_role"}, nil
}

func normalizeAndValidateGrant(dst *model.ResourceGrant, in GrantInput) error {
	st := strings.ToLower(strings.TrimSpace(in.SubjectType))
	if !model.IsValidSubjectType(st) {
		return fmt.Errorf("%w: subject_type must be user or org", errs.ErrInvalid)
	}
	if in.SubjectID == 0 {
		return fmt.Errorf("%w: subject_id must be > 0", errs.ErrInvalid)
	}
	rt := strings.ToLower(strings.TrimSpace(in.ResourceType))
	if !model.IsValidRBACResourceType(rt) {
		return fmt.Errorf("%w: invalid resource_type %q", errs.ErrInvalid, in.ResourceType)
	}
	pattern := strings.TrimSpace(in.ResourcePattern)
	if pattern == "" || len([]rune(pattern)) > 255 {
		return fmt.Errorf("%w: resource_pattern required (max 255 chars)", errs.ErrInvalid)
	}
	actions, err := normalizeActions(in.Actions)
	if err != nil {
		return err
	}
	effect := strings.ToLower(strings.TrimSpace(in.Effect))
	if !model.IsValidRBACEffect(effect) {
		return fmt.Errorf("%w: effect must be allow or deny", errs.ErrInvalid)
	}
	desc := strings.TrimSpace(in.Description)
	if len([]rune(desc)) > 255 {
		desc = string([]rune(desc)[:255])
	}

	dst.SubjectType = st
	dst.SubjectID = in.SubjectID
	dst.ResourceType = rt
	dst.ResourcePattern = pattern
	dst.Actions = actions
	dst.Effect = effect
	dst.Description = desc
	return nil
}

func normalizeActions(raw string) (string, error) {
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		a := strings.ToLower(strings.TrimSpace(p))
		if a == "" {
			continue
		}
		if !model.IsValidRBACAction(a) {
			return "", fmt.Errorf("%w: invalid action %q", errs.ErrInvalid, a)
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return "", fmt.Errorf("%w: at least one action required", errs.ErrInvalid)
	}
	return strings.Join(out, ","), nil
}

func actionsCover(grantActionsCSV, required string) bool {
	for _, raw := range strings.Split(grantActionsCSV, ",") {
		a := strings.ToLower(strings.TrimSpace(raw))
		switch a {
		case model.RBACActionAll:
			return true
		case model.RBACActionManage:
			return required == model.RBACActionRead || required == model.RBACActionWrite || required == model.RBACActionExec || required == model.RBACActionManage
		case model.RBACActionWrite:
			if required == model.RBACActionRead || required == model.RBACActionWrite {
				return true
			}
		default:
			if a == required {
				return true
			}
		}
	}
	return false
}

func matchesAnyKey(pattern string, candidates []string) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, c := range candidates {
		if matchWildcard(pattern, c) {
			return true
		}
	}
	return false
}

// matchWildcard 执行大小写不敏感的通配符匹配，* 匹配任意长度字符（含 /），? 匹配单字符。
func matchWildcard(pattern, target string) bool {
	p := []rune(strings.ToLower(strings.TrimSpace(pattern)))
	s := []rune(strings.ToLower(strings.TrimSpace(target)))
	pi, si := 0, 0
	starIdx, matchIdx := -1, 0
	for si < len(s) {
		if pi < len(p) && (p[pi] == '?' || p[pi] == s[si]) {
			pi++
			si++
		} else if pi < len(p) && p[pi] == '*' {
			starIdx = pi
			matchIdx = si
			pi++
		} else if starIdx != -1 {
			pi = starIdx + 1
			matchIdx++
			si = matchIdx
		} else {
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

func formatRuleSummary(g *model.ResourceGrant) string {
	return fmt.Sprintf("%s #%d %s %s(%s) on %s", g.SubjectType, g.SubjectID, g.Effect, g.ResourceType, g.Actions, g.ResourcePattern)
}
