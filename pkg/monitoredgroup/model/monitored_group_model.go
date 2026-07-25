package monitored_group_model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MonitoredGroup é a allowlist (por instância) de grupos a monitorar. Se uma instância
// não tem nenhuma linha, todos os grupos dela são monitorados (retrocompatível).
type MonitoredGroup struct {
	Id         string    `json:"id" gorm:"type:uuid;primaryKey"`
	InstanceId string    `json:"instance_id" gorm:"column:instance_id;uniqueIndex:idx_monitored_instance_group"`
	GroupJid   string    `json:"group_jid" gorm:"column:group_jid;uniqueIndex:idx_monitored_instance_group"`
	Name       string    `json:"name"`
	Enabled    bool      `json:"enabled" gorm:"default:true"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (m *MonitoredGroup) BeforeCreate(tx *gorm.DB) (err error) {
	if m.Id == "" {
		m.Id = uuid.New().String()
	}
	return
}
