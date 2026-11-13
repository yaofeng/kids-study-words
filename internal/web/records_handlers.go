package web

import (
	"net/http"
	"strconv"
)

// recordsData feeds the history page and its append fragment.
type recordsData struct {
	Base
	Groups     []DayGroup
	Filter     string
	HasMore    bool
	NextBefore int64
}

const recordsPageSize = 200

func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	userID := currentUser(r).ID

	filter := ""
	var notebookID *int64
	if raw := r.URL.Query().Get("notebook"); raw != "" {
		if id, ok := parseID(raw); ok {
			if nb, err := s.store.NotebookByID(r.Context(), userID, id); err == nil {
				notebookID = &nb.ID
				filter = strconv.FormatInt(nb.ID, 10)
			}
		}
	}

	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		if id, ok := parseID(raw); ok {
			before = id
		}
	}

	logs, err := s.store.ListRecallLogs(r.Context(), userID, notebookID, before, recordsPageSize)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := recordsData{
		Groups:  GroupLogs(logs, s.loc),
		Filter:  filter,
		HasMore: len(logs) == recordsPageSize,
	}
	if len(logs) > 0 {
		data.NextBefore = logs[len(logs)-1].ID
	}

	base, ok := s.pageData(w, r, "回忆记录", "records")
	if !ok {
		return
	}
	data.Base = base

	if isHtmx(r) {
		_ = s.renderer.fragment(w, http.StatusOK, "records_fragment", data)
		return
	}
	if err := s.renderer.page(w, http.StatusOK, "records", data); err != nil {
		s.fail(w, r, err)
	}
}
