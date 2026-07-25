package monitored_group_service

import (
	"time"

	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	monitored_group_model "github.com/evolution-foundation/evolution-go/pkg/monitoredgroup/model"
	monitored_group_repository "github.com/evolution-foundation/evolution-go/pkg/monitoredgroup/repository"
	"github.com/patrickmn/go-cache"
	"gorm.io/gorm"
)

type MonitoredGroupService interface {
	// IsMonitored: allowlist vazia (instância sem linhas) -> true (monitora tudo);
	// senão -> true só se o grupo estiver na lista.
	IsMonitored(instanceID, groupJID string) bool
	List(instanceID string) ([]monitored_group_model.MonitoredGroup, error)
	Add(instanceID, groupJID, name string) (*monitored_group_model.MonitoredGroup, error)
	Remove(instanceID, groupJID string) (int64, error)
}

type monitoredGroupService struct {
	repo          monitored_group_repository.MonitoredGroupRepository
	cache         *cache.Cache
	loggerWrapper *logger_wrapper.LoggerManager
}

func NewMonitoredGroupService(db *gorm.DB, loggerWrapper *logger_wrapper.LoggerManager) MonitoredGroupService {
	return &monitoredGroupService{
		repo:          monitored_group_repository.NewMonitoredGroupRepository(db),
		cache:         cache.New(30*time.Second, 1*time.Minute),
		loggerWrapper: loggerWrapper,
	}
}

// allowsetFor devolve o conjunto de group_jids habilitados de uma instância (cacheado 30s).
// Em erro de banco, cacheia vazio -> comportamento fail-open (monitora tudo).
func (s *monitoredGroupService) allowsetFor(instanceID string) map[string]bool {
	if v, ok := s.cache.Get(instanceID); ok {
		return v.(map[string]bool)
	}
	set := map[string]bool{}
	groups, err := s.repo.ListByInstance(instanceID)
	if err != nil {
		s.loggerWrapper.GetLogger(instanceID).LogError("[%s] Falha ao carregar monitored_groups: %v", instanceID, err)
	} else {
		for _, g := range groups {
			if g.Enabled {
				set[g.GroupJid] = true
			}
		}
	}
	s.cache.Set(instanceID, set, cache.DefaultExpiration)
	return set
}

func (s *monitoredGroupService) IsMonitored(instanceID, groupJID string) bool {
	set := s.allowsetFor(instanceID)
	if len(set) == 0 {
		return true
	}
	return set[groupJID]
}

func (s *monitoredGroupService) List(instanceID string) ([]monitored_group_model.MonitoredGroup, error) {
	return s.repo.ListByInstance(instanceID)
}

func (s *monitoredGroupService) Add(instanceID, groupJID, name string) (*monitored_group_model.MonitoredGroup, error) {
	group, err := s.repo.Upsert(monitored_group_model.MonitoredGroup{
		InstanceId: instanceID,
		GroupJid:   groupJID,
		Name:       name,
		Enabled:    true,
	})
	if err == nil {
		s.cache.Delete(instanceID)
	}
	return group, err
}

func (s *monitoredGroupService) Remove(instanceID, groupJID string) (int64, error) {
	n, err := s.repo.Delete(instanceID, groupJID)
	if err == nil {
		s.cache.Delete(instanceID)
	}
	return n, err
}
