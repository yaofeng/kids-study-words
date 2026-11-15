package web

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/yaofeng/kids-study-words/internal/store"
)

const maxGroupNameRunes = 40

// groupWord pairs a notebook word with whether it belongs to the group.
type groupWord struct {
	store.Word
	Checked bool
}

// groupPageData feeds web/templates/group.html.
type groupPageData struct {
	Base
	Notebook store.Notebook
	Group    store.WordGroup
	Words    []groupWord
	Error    string
}

// validGroupName reports whether a group name is 1–40 runes after trimming.
func validGroupName(name string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(name))
	return n >= 1 && n <= maxGroupNameRunes
}

// loadGroup parses {groupID} and loads a group that belongs to the user and
// to nb. It renders the error page itself and reports false on failure.
func (s *Server) loadGroup(w http.ResponseWriter, r *http.Request, nb store.Notebook) (store.WordGroup, bool) {
	groupID, ok := parseID(r.PathValue("groupID"))
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "分组不存在。")
		return store.WordGroup{}, false
	}
	g, err := s.store.WordGroupByID(r.Context(), currentUser(r).ID, groupID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && g.NotebookID != nb.ID) {
		s.renderError(w, r, http.StatusNotFound, "分组不存在。")
		return store.WordGroup{}, false
	}
	if err != nil {
		s.fail(w, r, err)
		return store.WordGroup{}, false
	}
	return g, true
}

// groupPageView builds the group page's template data. Every group page
// response goes through here so the page always has the same shape.
func (s *Server) groupPageView(w http.ResponseWriter, r *http.Request, nb store.Notebook, g store.WordGroup, errMsg string) (groupPageData, bool) {
	words, err := s.store.ListWords(r.Context(), nb.ID, "added")
	if err != nil {
		s.fail(w, r, err)
		return groupPageData{}, false
	}
	ids, err := s.store.GroupWordIDs(r.Context(), currentUser(r).ID, g.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "分组不存在。")
			return groupPageData{}, false
		}
		s.fail(w, r, err)
		return groupPageData{}, false
	}
	members := make(map[int64]bool, len(ids))
	for _, id := range ids {
		members[id] = true
	}
	out := make([]groupWord, len(words))
	for i, word := range words {
		out[i] = groupWord{Word: word, Checked: members[word.ID]}
	}
	base, ok := s.pageData(w, r, g.Name, "notebooks")
	if !ok {
		return groupPageData{}, false
	}
	return groupPageData{
		Base:     base,
		Notebook: nb,
		Group:    g,
		Words:    out,
		Error:    errMsg,
	}, true
}

// renderGroupPage renders the group page at status, optionally with an inline
// error. All group page responses share groupPageView's data shape.
func (s *Server) renderGroupPage(w http.ResponseWriter, r *http.Request, status int, nb store.Notebook, g store.WordGroup, errMsg string) {
	data, ok := s.groupPageView(w, r, nb, g, errMsg)
	if !ok {
		return
	}
	_ = s.renderer.page(w, status, "group", data)
}

func groupPath(nb store.Notebook, g store.WordGroup) string {
	return "/notebooks/" + itoa(nb.ID) + "/groups/" + itoa(g.ID)
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

func (s *Server) handleGroupPage(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadNotebook(w, r)
	if !ok {
		return
	}
	g, ok := s.loadGroup(w, r, nb)
	if !ok {
		return
	}
	s.renderGroupPage(w, r, http.StatusOK, nb, g, "")
}

func (s *Server) handleRenameGroup(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadNotebook(w, r)
	if !ok {
		return
	}
	g, ok := s.loadGroup(w, r, nb)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if !validGroupName(name) {
		s.renderGroupPage(w, r, http.StatusOK, nb, g, "名称需为 1–40 个字符。")
		return
	}
	if _, err := s.store.RenameWordGroup(r.Context(), currentUser(r).ID, g.ID, name); err != nil {
		switch {
		case errors.Is(err, store.ErrDuplicate):
			s.renderGroupPage(w, r, http.StatusOK, nb, g, "已经有同名的分组了。")
		case errors.Is(err, store.ErrNotFound):
			s.renderError(w, r, http.StatusNotFound, "分组不存在。")
		default:
			s.fail(w, r, err)
		}
		return
	}
	setFlash(w, "分组已改名。")
	http.Redirect(w, r, groupPath(nb, g), http.StatusSeeOther)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadNotebook(w, r)
	if !ok {
		return
	}
	g, ok := s.loadGroup(w, r, nb)
	if !ok {
		return
	}
	if err := s.store.DeleteWordGroup(r.Context(), currentUser(r).ID, g.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "分组不存在。")
			return
		}
		s.fail(w, r, err)
		return
	}
	setFlash(w, "分组已删除。")
	http.Redirect(w, r, "/notebooks/"+itoa(nb.ID), http.StatusSeeOther)
}

func (s *Server) handleSaveGroupWords(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadNotebook(w, r)
	if !ok {
		return
	}
	g, ok := s.loadGroup(w, r, nb)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	var ids []int64
	for _, raw := range r.PostForm["word_id"] {
		id, ok := parseID(raw)
		if !ok {
			s.renderGroupPage(w, r, http.StatusBadRequest, nb, g, "单词参数不对。")
			return
		}
		ids = append(ids, id)
	}
	if err := s.store.SetGroupWords(r.Context(), currentUser(r).ID, g.ID, ids); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderGroupPage(w, r, http.StatusNotFound, nb, g, "有单词不属于这个生词本。")
			return
		}
		s.fail(w, r, err)
		return
	}
	setFlash(w, "分组已更新。")
	http.Redirect(w, r, groupPath(nb, g), http.StatusSeeOther)
}
