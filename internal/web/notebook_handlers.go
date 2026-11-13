package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yaofeng/kids-study-words/internal/store"
)

const maxNotebookNameRunes = 40

func (s *Server) handleNotebooksPage(w http.ResponseWriter, r *http.Request) {
	data, ok := s.pageData(w, r, "生词本", "notebooks")
	if !ok {
		return
	}
	if err := s.renderer.page(w, http.StatusOK, "notebooks", data); err != nil {
		s.fail(w, r, err)
	}
}

// renderNotebooksError re-renders the list with a message at the top.
func (s *Server) renderNotebooksError(w http.ResponseWriter, r *http.Request, msg string) {
	data, ok := s.pageData(w, r, "生词本", "notebooks")
	if !ok {
		return
	}
	data.Error = msg
	_ = s.renderer.page(w, http.StatusOK, "notebooks", data)
}

func validNotebookName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n > 0 && n <= maxNotebookNameRunes
}

func (s *Server) handleCreateNotebook(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if !validNotebookName(name) {
		s.renderNotebooksError(w, r, "名称需为 1–40 个字符。")
		return
	}
	if _, err := s.store.CreateNotebook(r.Context(), currentUser(r).ID, name); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			s.renderNotebooksError(w, r, "已经有同名的生词本了。")
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/notebooks", http.StatusSeeOther)
}

func (s *Server) handleRenameNotebook(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if !validNotebookName(name) {
		s.renderNotebooksError(w, r, "名称需为 1–40 个字符。")
		return
	}
	_, err := s.store.RenameNotebook(r.Context(), currentUser(r).ID, id, name)
	switch {
	case errors.Is(err, store.ErrDuplicate):
		s.renderNotebooksError(w, r, "已经有同名的生词本了。")
		return
	case errors.Is(err, store.ErrNotFound):
		s.renderError(w, r, http.StatusNotFound, "生词本不存在。")
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/notebooks", http.StatusSeeOther)
}

func (s *Server) handleSetDefaultNotebook(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	err := s.store.SetDefaultNotebook(r.Context(), currentUser(r).ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.renderError(w, r, http.StatusNotFound, "生词本不存在。")
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	setFlash(w, "已设为默认生词本。")
	http.Redirect(w, r, "/notebooks", http.StatusSeeOther)
}

func (s *Server) handleDeleteNotebook(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteNotebook(r.Context(), currentUser(r).ID, id)
	switch {
	case errors.Is(err, store.ErrLastNotebook):
		s.renderNotebooksError(w, r, "这是最后一个生词本，不能删除。")
		return
	case errors.Is(err, store.ErrNotFound):
		s.renderError(w, r, http.StatusNotFound, "生词本不存在。")
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	setFlash(w, "生词本已删除。")
	http.Redirect(w, r, "/notebooks", http.StatusSeeOther)
}

// pathID parses the {id} path value or renders a 404.
func (s *Server) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.renderError(w, r, http.StatusNotFound, "页面不存在，检查一下网址吧。")
		return 0, false
	}
	return id, true
}
