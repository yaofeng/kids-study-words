package web

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/yaofeng/kids-study-words/internal/store"
)

const maxGroupNameRunes = 40

// validGroupName reports whether a group name is 1–40 runes after trimming.
func validGroupName(name string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(name))
	return n >= 1 && n <= maxGroupNameRunes
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadNotebook(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if !validGroupName(name) {
		s.renderNotebookDetail(w, r, nb, "added", "名称需为 1–40 个字符。")
		return
	}
	if _, err := s.store.CreateWordGroup(r.Context(), nb.ID, name); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			s.renderNotebookDetail(w, r, nb, "added", "已经有同名的分组了。")
			return
		}
		s.fail(w, r, err)
		return
	}
	setFlash(w, "分组已创建。")
	http.Redirect(w, r, "/notebooks/"+itoa(nb.ID), http.StatusSeeOther)
}
