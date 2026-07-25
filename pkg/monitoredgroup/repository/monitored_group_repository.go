package monitored_group_repository

import (
	monitored_group_model "github.com/evolution-foundation/evolution-go/pkg/monitoredgroup/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MonitoredGroupRepository interface {
	ListByInstance(instanceID string) ([]monitored_group_model.MonitoredGroup, error)
	Upsert(group monitored_group_model.MonitoredGroup) (*monitored_group_model.MonitoredGroup, error)
	Delete(instanceID, groupJID string) (int64, error)
}

type monitoredGroupRepository struct {
	db *gorm.DB
}

func NewMonitoredGroupRepository(db *gorm.DB) MonitoredGroupRepository {
	return &monitoredGroupRepository{db: db}
}

func (r *monitoredGroupRepository) ListByInstance(instanceID string) ([]monitored_group_model.MonitoredGroup, error) {
	var groups []monitored_group_model.MonitoredGroup
	err := r.db.Where("instance_id = ?", instanceID).Order("created_at DESC").Find(&groups).Error
	return groups, err
}

func (r *monitoredGroupRepository) Upsert(group monitored_group_model.MonitoredGroup) (*monitored_group_model.MonitoredGroup, error) {
	err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "instance_id"}, {Name: "group_jid"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "enabled"}),
	}).Create(&group).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *monitoredGroupRepository) Delete(instanceID, groupJID string) (int64, error) {
	result := r.db.Where("instance_id = ? AND group_jid = ?", instanceID, groupJID).
		Delete(&monitored_group_model.MonitoredGroup{})
	return result.RowsAffected, result.Error
}
