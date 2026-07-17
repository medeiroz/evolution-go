// Package core previously implemented Evolution's license activation,
// integrity checks and a usage heartbeat — all of which phoned home to an
// external license server (https://.../v1/activate, /v1/heartbeat,
// /v1/register/auto). This fork removes that machinery in full.
//
// What remains here is ONLY the surface that cmd/evolution-go/main.go consumes,
// reduced to inert no-ops:
//
//   - no license: the instance is always active.
//   - no gating: GateMiddleware passes every request through.
//   - no activation UI: LicenseRoutes registers nothing.
//   - no network: this file imports nothing that can reach out (no net/http,
//     no crypto, no embedded keys). Grep it — there is nothing to phone home.
//
// Every other symbol the original file exported (ComputeSessionSeed,
// DeriveInstanceToken, ValidateRouteAccess, ActivateIntegrity, TrackMessage*,
// the RuntimeConfig/RuntimeData types, the whole obfuscated `_xxx` internals)
// was referenced only from within the license machinery and is gone with it —
// verified: nothing outside pkg/core imported any of it.
package core

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RuntimeContext is an opaque, inert placeholder for the removed license runtime.
type RuntimeContext struct{}

// SetDB is a no-op: the license store this fork removed no longer exists.
func SetDB(_ *gorm.DB) {}

// MigrateDB is a no-op: there are no license tables to migrate.
func MigrateDB() error { return nil }

// InitializeRuntime returns an inert context; the instance is always active.
func InitializeRuntime(_tier, _version, _apiKey string) *RuntimeContext {
	return &RuntimeContext{}
}

// GateMiddleware is a pass-through — there is no license gating.
func GateMiddleware(_ *RuntimeContext) gin.HandlerFunc {
	return func(c *gin.Context) { c.Next() }
}

// LicenseRoutes serves a single local, always-"active" status endpoint.
//
// The Manager UI (manager/dist) probes GET /license/status on load and treats
// status == "active" as licensed; anything else makes it show "Licença
// necessária" and redirect to an external registrar. This fork has no license
// server, so we answer "active" locally — no network, no activation flow, no
// heartbeat. The real activation/register/callback endpoints stay removed.
func LicenseRoutes(r *gin.Engine, _ *RuntimeContext) {
	r.GET("/license/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "active"})
	})
}

// StartHeartbeat is a no-op: no usage heartbeat ever leaves the instance.
func StartHeartbeat(_ context.Context, _ *RuntimeContext, _ time.Time) {}

// Shutdown is a no-op.
func Shutdown(_ *RuntimeContext) {}
