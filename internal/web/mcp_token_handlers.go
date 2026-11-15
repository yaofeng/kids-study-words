package web

import (
	"net/http"

	"github.com/yaofeng/kids-study-words/internal/auth"
)

// handleGenerateMCPToken issues a new MCP access token, stores only its hash,
// and re-renders the settings page with the plaintext shown exactly once.
func (s *Server) handleGenerateMCPToken(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r).ID
	raw, hash, err := auth.NewPrefixedToken("mcp_", 32)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.ReplaceMCPToken(r.Context(), userID, hash); err != nil {
		s.fail(w, r, err)
		return
	}
	data, ok := s.settingsData(w, r)
	if !ok {
		return
	}
	data.NewMCPToken = raw
	w.Header().Set("Cache-Control", "no-store")
	if err := s.renderer.page(w, http.StatusOK, "settings", data); err != nil {
		s.fail(w, r, err)
	}
}

// handleRevokeMCPToken deletes the user's MCP access token.
func (s *Server) handleRevokeMCPToken(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RevokeMCPToken(r.Context(), currentUser(r).ID); err != nil {
		s.fail(w, r, err)
		return
	}
	setFlash(w, "MCP 令牌已撤销。")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
