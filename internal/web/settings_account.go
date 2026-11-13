package web

import (
	"net/http"

	"github.com/yaofeng/kids-study-words/internal/auth"
)

// repoURL is shown on the settings page and in the about section.
const repoURL = "https://github.com/yaofeng/kids-study-words"

// settingsView is the template data for the settings page.
type settingsView struct {
	Base
	RepoURL   string
	AIEnabled bool
	AIModel   string
	AIHost    string
}

func (s *Server) settingsData(w http.ResponseWriter, r *http.Request) (settingsView, bool) {
	base, ok := s.pageData(w, r, "设置", "settings")
	if !ok {
		return settingsView{}, false
	}
	view := settingsView{Base: base, RepoURL: repoURL}
	if s.ai != nil && s.ai.Enabled() {
		view.AIEnabled = true
		view.AIModel = s.ai.Model()
		view.AIHost = s.ai.Host()
	}
	return view, true
}

func (s *Server) renderSettingsData(w http.ResponseWriter, r *http.Request, status int, msg string) {
	data, ok := s.settingsData(w, r)
	if !ok {
		return
	}
	data.Error = msg
	_ = s.renderer.page(w, status, "settings", data)
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	data, ok := s.settingsData(w, r)
	if !ok {
		return
	}
	if err := s.renderer.page(w, http.StatusOK, "settings", data); err != nil {
		s.fail(w, r, err)
	}
}

// handleChangePassword verifies the current password, stores a new one, and
// signs out every other session.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	userID := currentUser(r).ID

	user, err := s.store.UserByID(r.Context(), userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	current := r.PostFormValue("current")
	next := r.PostFormValue("new")
	confirm := r.PostFormValue("confirm")

	if !auth.CheckPassword(user.PasswordHash, current) {
		s.renderSettingsData(w, r, http.StatusOK, "当前密码不对。")
		return
	}
	if !auth.ValidPassword(next) {
		s.renderSettingsData(w, r, http.StatusOK, "新密码需为 6–72 个字符。")
		return
	}
	if next != confirm {
		s.renderSettingsData(w, r, http.StatusOK, "两次输入的新密码不一致。")
		return
	}

	hash, err := auth.HashPassword(next)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.UpdatePassword(r.Context(), userID, hash); err != nil {
		s.fail(w, r, err)
		return
	}

	// Keep this browser signed in; drop every other session.
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if err := s.store.DeleteSessionsForUserExcept(r.Context(), userID, auth.HashToken(c.Value)); err != nil {
			s.fail(w, r, err)
			return
		}
	} else if err := s.store.DeleteSessionsForUser(r.Context(), userID); err != nil {
		s.fail(w, r, err)
		return
	}

	setFlash(w, "密码已更新。")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
