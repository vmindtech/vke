package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IAuditLogRepository interface {
	CreateAuditLog(ctx context.Context, auditLog *model.AuditLog) error
}

type AuditLogRepository struct {
	mysqlInstance mysqldb.IMysqlInstance
}

func NewAuditLogRepository(mysqlInstance mysqldb.IMysqlInstance) *AuditLogRepository {
	return &AuditLogRepository{
		mysqlInstance: mysqlInstance,
	}
}

func (a *AuditLogRepository) CreateAuditLog(ctx context.Context, auditLog *model.AuditLog) error {
	return a.mysqlInstance.
		Database().
		WithContext(ctx).
		Create(auditLog).
		Error
}
