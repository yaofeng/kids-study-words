package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// renderSettingsError re-renders the settings page with a message.
func (s *Server) renderSettingsError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	s.renderSettingsData(w, r, status, msg)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r).ID
	param := r.URL.Query().Get("notebook")

	var (
		f   store.ExportFile
		err error
	)
	switch param {
	case "", "all":
		f, err = s.store.ExportAll(r.Context(), userID)
	default:
		id, convErr := strconv.ParseInt(param, 10, 64)
		if convErr != nil || id <= 0 {
			s.renderError(w, r, http.StatusBadRequest, "生词本参数不对。")
			return
		}
		f, err = s.store.ExportNotebook(r.Context(), userID, id)
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.renderError(w, r, http.StatusNotFound, "生词本不存在。")
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}

	payload, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := "study-words-" + s.now().In(s.loc).Format("20060102") + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
