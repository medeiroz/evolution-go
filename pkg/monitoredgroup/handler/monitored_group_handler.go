package monitored_group_handler

import (
	"net/http"

	logger_wrapper "github.com/evolution-foundation/evolution-go/pkg/logger"
	monitored_group_service "github.com/evolution-foundation/evolution-go/pkg/monitoredgroup/service"
	"github.com/gin-gonic/gin"
)

type MonitoredGroupHandler struct {
	service       monitored_group_service.MonitoredGroupService
	loggerWrapper *logger_wrapper.LoggerManager
}

func NewMonitoredGroupHandler(service monitored_group_service.MonitoredGroupService, loggerWrapper *logger_wrapper.LoggerManager) *MonitoredGroupHandler {
	return &MonitoredGroupHandler{service: service, loggerWrapper: loggerWrapper}
}

type monitoredGroupBody struct {
	GroupJid string `json:"group_jid"`
	Name     string `json:"name"`
}

// List godoc
// @Router /instance/monitored-groups/{instanceId} [get]
func (h *MonitoredGroupHandler) List(ctx *gin.Context) {
	instanceID := ctx.Param("instanceId")
	groups, err := h.service.List(instanceID)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": groups})
}

// Add godoc
// @Router /instance/monitored-groups/{instanceId} [post]
func (h *MonitoredGroupHandler) Add(ctx *gin.Context) {
	instanceID := ctx.Param("instanceId")
	var body monitoredGroupBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.GroupJid == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "group_jid is required"})
		return
	}
	group, err := h.service.Add(instanceID, body.GroupJid, body.Name)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"data": group})
}

// Remove godoc
// @Router /instance/monitored-groups/{instanceId} [delete]
func (h *MonitoredGroupHandler) Remove(ctx *gin.Context) {
	instanceID := ctx.Param("instanceId")
	var body monitoredGroupBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.GroupJid == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "group_jid is required"})
		return
	}
	n, err := h.service.Remove(instanceID, body.GroupJid)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"deleted": n})
}
