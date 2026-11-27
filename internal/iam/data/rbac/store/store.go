// Package store 提供资源级 RBAC 规则（iam_resource_grants）的 GORM 持久化实现。
package store

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/Zara1024/OpsPilot/internal/iam/model"
	"github.com/Zara1024/OpsPilot/internal/pkg/dbx"
	"github.com/Zara1024/OpsPilot/internal/pkg/errs"
)

// Migrate 执行 iam_resource_grants 表的自动迁移，严格兼容 SQLite 与 MySQL 8.0。
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&model.ResourceGrant{})
}

// ListFilter 兼容别名，真实定义位于 model.ResourceGrantFilter。
type ListFilter = model.ResourceGrantFilter

// Store 封装 iam_resource_grants CRUD 与主体匹配查询。
type Store struct {
	db *gorm.DB
}

// New 构造 RBAC Store。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// List 按条件列出资源授权规则。
func (s *Store) List(ctx context.Context, f model.ResourceGrantFilter) ([]*model.ResourceGrant, error) {
	q := dbx.WithContext(ctx, s.db).Model(&model.ResourceGrant{})
	if st := strings.TrimSpace(f.SubjectType); st != "" {
		q = q.Where("subject_type = ?", st)
	}
	if f.SubjectID > 0 {
		q = q.Where("subject_id = ?", f.SubjectID)
	}
	if rt := strings.TrimSpace(f.ResourceType); rt != "" {
		q = q.Where("resource_type = ?", rt)
	}
	if ef := strings.TrimSpace(f.Effect); ef != "" {
		q = q.Where("effect = ?", ef)
	}
	var out []*model.ResourceGrant
	if err := q.Order("id desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ListForSubjects 返回指定用户及所属组织名下针对某 resourceType 的全部授权规则。
func (s *Store) ListForSubjects(ctx context.Context, userID uint64, orgIDs []uint64, resourceType string) ([]*model.ResourceGrant, error) {
	q := dbx.WithContext(ctx, s.db).Model(&model.ResourceGrant{}).Where("resource_type = ?", resourceType)
	if len(orgIDs) > 0 {
		q = q.Where(
			"(subject_type = ? AND subject_id = ?) OR (subject_type = ? AND subject_id IN ?)",
			model.SubjectTypeUser, userID, model.SubjectTypeOrg, orgIDs,
		)
	} else {
		q = q.Where("subject_type = ? AND subject_id = ?", model.SubjectTypeUser, userID)
	}
	var out []*model.ResourceGrant
	if err := q.Order("id asc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetByID 按 ID 获取一条规则。
func (s *Store) GetByID(ctx context.Context, id uint64) (*model.ResourceGrant, error) {
	var row model.ResourceGrant
	if err := dbx.WithContext(ctx, s.db).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

// Create 新增规则。
func (s *Store) Create(ctx context.Context, g *model.ResourceGrant) error {
	if g == nil {
		return errs.ErrInvalid
	}
	return dbx.WithContext(ctx, s.db).Create(g).Error
}

// Update 保存规则更新。
func (s *Store) Update(ctx context.Context, g *model.ResourceGrant) error {
	if g == nil || g.ID == 0 {
		return errs.ErrInvalid
	}
	return dbx.WithContext(ctx, s.db).Save(g).Error
}

// Delete 删除指定规则。
func (s *Store) Delete(ctx context.Context, id uint64) error {
	res := dbx.WithContext(ctx, s.db).Delete(&model.ResourceGrant{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.ErrNotFound
	}
	return nil
}
