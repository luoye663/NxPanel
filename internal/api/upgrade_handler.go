package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/update"
	"github.com/luoye663/nxpanel/internal/upgrade"
)

const upgradeChannelKey = "panel_upgrade_channel"

func (s *Server) enrichUpdateStatus(ctx context.Context, status *upgrade.UpgradeStatus) *upgrade.UpgradeStatus {
	status.Docker = os.Getenv("NXPANEL_UPDATE_DOCKER") == "1"
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	state, err := update.NewClient(update.Home()).Status(ctx)
	status.Managed = err == nil
	status.Available = status.Available && status.Managed && !state.Busy() && state.Phase != update.Prepared
	if err == nil && state.Phase != update.Idle {
		status.Task = &update.State{ID: state.ID, Phase: state.Phase, Version: state.Version, Error: state.Error}
	} else if err != nil {
		state = update.State{}
	}
	status.Progress = s.upgradeSvc.Progress(state)
	return status
}

func (s *Server) handleUpgradeChannel(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Channel string `json:"channel"`
	}
	if !DecodeJSON(w, r, &request) {
		return
	}
	if request.Channel != "stable" && request.Channel != "preview" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "更新通道无效", nil)
		return
	}
	s.updateMu.Lock()
	err := s.settingsRepo.Set(upgradeChannelKey, request.Channel)
	if err == nil {
		s.upgradeSvc.SetChannel(request.Channel)
	}
	s.updateMu.Unlock()
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, "无法保存更新通道", nil)
		return
	}
	s.recordUpgradeAction(r, "upgrade_channel", request.Channel, "")
	s.handleUpgradeCheckTrigger(w, r)
}

func (s *Server) handleUpgradePrepare(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Version string `json:"version"`
	}
	if !DecodeJSON(w, r, &request) {
		return
	}
	if err := s.upgradeSvc.Prepare(request.Version, update.NewClient(update.Home())); err != nil {
		WriteError(w, r, http.StatusConflict, app.ErrBadRequest, err.Error(), nil)
		return
	}
	s.recordUpgradeAction(r, "upgrade_prepare", request.Version, "")
	s.handleUpgradeCheck(w, r)
}

func (s *Server) handleUpgradeCancel(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if !DecodeJSON(w, r, &request) {
		return
	}
	if _, err := update.NewClient(update.Home()).Act(r.Context(), "cancel", update.Action{ID: request.ID}); err != nil {
		WriteError(w, r, http.StatusConflict, app.ErrBadRequest, "无法取消：更新任务已改变或不在待重启状态", nil)
		return
	}
	s.upgradeSvc.RecordCancel(request.ID)
	s.recordUpgradeAction(r, "upgrade_cancel", request.ID, "")
	s.handleUpgradeCheck(w, r)
}

func (s *Server) handleUpgradeActivate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if !DecodeJSON(w, r, &request) {
		return
	}
	client := update.NewClient(update.Home())
	state, err := client.Status(r.Context())
	if err != nil || state.Phase != update.Prepared || request.ID != state.ID {
		WriteError(w, r, http.StatusConflict, app.ErrBadRequest, "更新任务已改变或不在待重启状态", nil)
		return
	}
	if _, err := s.upgradeSvc.RefreshTrust(r.Context(), client); err != nil {
		WriteError(w, r, http.StatusConflict, app.ErrBadRequest, "无法验证最新更新授权，请稍后重试", nil)
		return
	}
	if !s.pauseForUpgrade() {
		WriteError(w, r, http.StatusConflict, app.ErrBadRequest, "面板仍有写入或后台任务在运行，请稍后再重启", nil)
		return
	}
	auditID := "upgrade-" + state.ID
	s.recordUpgradeAction(r, "upgrade_activate", state.Version, auditID)
	path, err := filepath.Abs(filepath.Join(s.cfg.DataDir, "panel.db"))
	if err == nil {
		var activated update.State
		activated, err = client.Act(r.Context(), "activate", update.Action{ID: state.ID, DBPath: path})
		if err == nil {
			s.upgradeSvc.Progress(activated)
		}
	}
	if err != nil {
		persisted, readErr := update.ReadState(update.Home())
		if readErr == nil && !persisted.Busy() {
			s.resumeAfterUpgradeFailure()
			_ = s.opRepo.UpdateError(auditID, "failed", "UPGRADE_NOT_STARTED", "更新未能启动", "")
			WriteError(w, r, http.StatusConflict, app.ErrBadRequest, "更新未能启动，请重新查询状态", nil)
			return
		}
		if readErr == nil && persisted.ID == state.ID {
			s.upgradeSvc.Progress(persisted)
		}
	}
	WriteOK(w, r, map[string]string{"id": state.ID, "version": state.Version})
}

func (s *Server) guardUpdateWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if update.Home() == "" || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || strings.HasSuffix(r.URL.Path, "/system/upgrade/activate") {
			next.ServeHTTP(w, r)
			return
		}
		s.updateMu.Lock()
		if s.updatePausing {
			s.updateMu.Unlock()
			WriteError(w, r, http.StatusServiceUnavailable, app.ErrBadRequest, "面板正在应用更新", nil)
			return
		}
		s.updateWrites++
		s.updateMu.Unlock()
		defer func() {
			s.updateMu.Lock()
			s.updateWrites--
			s.updateMu.Unlock()
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) pauseForUpgrade() bool {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updatePausing || s.updateWrites != 0 || !s.scheduledTaskRunner.PauseIfIdle() {
		return false
	}
	if (s.acmeSvc != nil && s.acmeSvc.Busy()) || (s.siteBackupSvc != nil && s.siteBackupSvc.Busy()) {
		s.scheduledTaskRunner.Resume()
		return false
	}
	s.updatePausing = true
	return true
}

func (s *Server) resumeAfterUpgradeFailure() {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	s.scheduledTaskRunner.Resume()
	s.updatePausing = false
}

func (s *Server) recordUpgradeAction(r *http.Request, action, target, id string) {
	status := "success"
	if id == "" {
		id = app.NewID("op")
	} else {
		status = "pending"
	}
	err := s.opRepo.CreateContext(r.Context(), &repo.Operation{
		ID: id, Action: action, TargetType: "panel_update", TargetID: target, Status: status,
		RequestID: middleware.GetRequestID(r.Context()), Actor: "admin", IP: middleware.GetRealIP(r.Context()),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		slog.Warn("无法记录更新操作", "action", action, "error", err)
	}
}

func (s *Server) finishUpgradeAudit() {
	state, err := update.ReadState(update.Home())
	if err != nil || state.ID == "" {
		return
	}
	auditID := "upgrade-" + state.ID
	record, err := s.opRepo.GetByID(auditID)
	if err != nil || record == nil || record.Status != "pending" {
		return
	}
	switch state.Phase {
	case update.Committed:
		_ = s.opRepo.UpdateStatus(auditID, "success")
	case update.RolledBack:
		_ = s.opRepo.UpdateError(auditID, "failed", "UPGRADE_ROLLED_BACK", state.Error, "")
	}
}
