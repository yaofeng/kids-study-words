package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// scopeOption is one proficiency threshold choice on the scope page.
type scopeOption struct {
	Level int
	Label string
	Count int
}

// recallStartGroup is a group with its checkbox state.
type recallStartGroup struct {
	store.WordGroup
	Checked bool
}

// recallStartData feeds the recall scope selection page.
type recallStartData struct {
	Base
	Notebook            store.Notebook
	AllCount            int
	Options             []scopeOption
	Groups              []recallStartGroup
	AllSelected         bool
	ProficiencySelected bool
	GroupsSelected      bool
	MaxProficiency      int
	Error               string
}

// parseRecallScope decodes the scope form or query values.
func parseRecallScope(v url.Values) (store.RecallScope, error) {
	switch scope := strings.TrimSpace(v.Get("scope")); scope {
	case "", "all":
		return store.RecallScope{Kind: store.ScopeAll}, nil
	case "proficiency":
		raw := strings.TrimSpace(v.Get("max"))
		if raw == "" {
			return store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6}, nil
		}
		level, err := strconv.Atoi(raw)
		if err != nil || level < 2 || level > 10 {
			return store.RecallScope{}, store.ErrInvalidScope
		}
		return store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: level}, nil
	case "groups":
		ids := make([]int64, 0, len(v["group_id"]))
		for _, raw := range v["group_id"] {
			id, ok := parseID(raw)
			if !ok {
				return store.RecallScope{}, store.ErrInvalidScope
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return store.RecallScope{}, store.ErrInvalidScope
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		unique := ids[:0]
		for i, id := range ids {
			if i == 0 || id != ids[i-1] {
				unique = append(unique, id)
			}
		}
		return store.RecallScope{Kind: store.ScopeGroups, GroupIDs: unique}, nil
	default:
		return store.RecallScope{}, store.ErrInvalidScope
	}
}

// groupsScopeWithoutSelection reports the specific user mistake of choosing the
// groups scope without checking any group. Genuinely malformed input (unknown
// scope, invalid group ids) is not reported here.
func groupsScopeWithoutSelection(v url.Values) bool {
	return strings.TrimSpace(v.Get("scope")) == "groups" && len(v["group_id"]) == 0
}

// scopeQuery encodes a scope for recall links, sorting group ids first.
func scopeQuery(scope store.RecallScope) url.Values {
	v := url.Values{}
	switch scope.Kind {
	case store.ScopeProficiency:
		v.Set("scope", "proficiency")
		v.Set("max", strconv.Itoa(scope.MaxProficiency))
	case store.ScopeGroups:
		v.Set("scope", "groups")
		ids := append([]int64(nil), scope.GroupIDs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			v.Add("group_id", strconv.FormatInt(id, 10))
		}
	default:
		v.Set("scope", "all")
	}
	return v
}

// formatStars renders a 1–10 proficiency level as a star count.
func formatStars(level int) string {
	if level%2 == 0 {
		return strconv.Itoa(level / 2)
	}
	return strconv.FormatFloat(float64(level)/2, 'f', 1, 64)
}

// scopeLabel is the human-readable summary of a scope.
func scopeLabel(scope store.RecallScope, groups []store.WordGroup) string {
	switch scope.Kind {
	case store.ScopeProficiency:
		return "低于 " + formatStars(scope.MaxProficiency) + " 星"
	case store.ScopeGroups:
		names := make(map[int64]string, len(groups))
		for _, g := range groups {
			names[g.ID] = g.Name
		}
		picked := make([]string, 0, len(scope.GroupIDs))
		for _, id := range scope.GroupIDs {
			if name, ok := names[id]; ok {
				picked = append(picked, name)
			}
		}
		if len(picked) == 0 {
			return "分组"
		}
		if len(scope.GroupIDs) > 3 {
			// Truncate by resolved names, never by the requested ids: a group
			// may have been deleted and leave fewer names than ids.
			shown := len(picked)
			if shown > 3 {
				shown = 3
			}
			return strings.Join(picked[:shown], "、") + fmt.Sprintf(" 等 %d 组", len(scope.GroupIDs))
		}
		return strings.Join(picked, "、")
	default:
		return "全部"
	}
}

// recallStartView assembles the scope page data and counts. It renders store
// failures through the error page and reports false.
func (s *Server) recallStartView(w http.ResponseWriter, r *http.Request, nb store.Notebook, scope store.RecallScope, errMsg string) (recallStartData, bool) {
	words, err := s.store.ListWords(r.Context(), nb.ID, "added")
	if err != nil {
		s.fail(w, r, err)
		return recallStartData{}, false
	}
	groups, err := s.store.ListWordGroups(r.Context(), nb.ID)
	if err != nil {
		s.fail(w, r, err)
		return recallStartData{}, false
	}
	base, ok := s.pageData(w, r, nb.Name, "recall")
	if !ok {
		return recallStartData{}, false
	}

	data := recallStartData{
		Base:           base,
		Notebook:       nb,
		AllCount:       len(words),
		MaxProficiency: 6,
		Error:          errMsg,
	}
	for level := 2; level <= 10; level++ {
		count := 0
		for _, word := range words {
			if word.Proficiency < level {
				count++
			}
		}
		data.Options = append(data.Options, scopeOption{
			Level: level,
			Label: "低于 " + formatStars(level) + " 星",
			Count: count,
		})
	}
	switch scope.Kind {
	case store.ScopeProficiency:
		data.ProficiencySelected = true
		data.MaxProficiency = scope.MaxProficiency
	case store.ScopeGroups:
		data.GroupsSelected = true
	default:
		data.AllSelected = true
	}
	data.Groups = make([]recallStartGroup, 0, len(groups))
	for _, g := range groups {
		data.Groups = append(data.Groups, recallStartGroup{
			WordGroup: g,
			Checked:   groupIDIn(scope.GroupIDs, g.ID),
		})
	}
	return data, true
}

// groupIDIn reports whether id is in ids.
func groupIDIn(ids []int64, id int64) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

// scopeGroupsExist reports whether every group named by a group scope still
// belongs to the notebook. Foreign or deleted groups make the scope stale.
func (s *Server) scopeGroupsExist(ctx context.Context, notebookID int64, scope store.RecallScope) (bool, error) {
	if scope.Kind != store.ScopeGroups {
		return true, nil
	}
	groups, err := s.store.ListWordGroups(ctx, notebookID)
	if err != nil {
		return false, err
	}
	present := make(map[int64]bool, len(groups))
	for _, g := range groups {
		present[g.ID] = true
	}
	for _, id := range scope.GroupIDs {
		if !present[id] {
			return false, nil
		}
	}
	return true, nil
}

// changeScopeURL links back to the scope page for a round's current scope.
func changeScopeURL(notebookID int64, scope store.RecallScope) string {
	return "/recall/" + strconv.FormatInt(notebookID, 10) + "/start?" + scopeQuery(scope).Encode()
}

// renderRecallStart shows the scope page with an inline error.
func (s *Server) renderRecallStart(w http.ResponseWriter, r *http.Request, nb store.Notebook, scope store.RecallScope, errMsg string) {
	data, ok := s.recallStartView(w, r, nb, scope, errMsg)
	if !ok {
		return
	}
	if err := s.renderer.page(w, http.StatusOK, "recall_start", data); err != nil {
		s.fail(w, r, err)
	}
}

func (s *Server) handleRecallStartPage(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadRecallNotebook(w, r)
	if !ok {
		return
	}
	scope, err := parseRecallScope(r.URL.Query())
	if errors.Is(err, store.ErrInvalidScope) {
		s.renderError(w, r, http.StatusBadRequest, "范围参数不对。")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderRecallStart(w, r, nb, scope, "")
}

func (s *Server) handleRecallStartPost(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadRecallNotebook(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	scope, err := parseRecallScope(r.PostForm)
	if errors.Is(err, store.ErrInvalidScope) {
		if groupsScopeWithoutSelection(r.PostForm) {
			s.renderRecallStart(w, r, nb, store.RecallScope{Kind: store.ScopeGroups}, "请至少选择一个分组。")
			return
		}
		s.renderError(w, r, http.StatusBadRequest, "范围参数不对。")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	groupsOK, err := s.scopeGroupsExist(r.Context(), nb.ID, scope)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !groupsOK {
		s.renderRecallStart(w, r, nb, scope, "分组已经不存在，请重新选择。")
		return
	}
	words, err := s.store.ListRecallWords(r.Context(), nb.ID, scope)
	if errors.Is(err, store.ErrInvalidScope) {
		s.renderError(w, r, http.StatusBadRequest, "范围参数不对。")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(words) == 0 {
		s.renderRecallStart(w, r, nb, scope, "这个范围里还没有单词。")
		return
	}
	s.recall.Start(currentUser(r).ID, nb.ID, scope)
	http.Redirect(w, r, "/recall/"+strconv.FormatInt(nb.ID, 10), http.StatusSeeOther)
}
