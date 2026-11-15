package web

import (
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yaofeng/kids-study-words/internal/recall"
	"github.com/yaofeng/kids-study-words/internal/store"
)

// recallView feeds both the recall page and its htmx fragments.
type recallView struct {
	Base
	Notebook       store.Notebook
	Word           *store.Word
	State          recall.State
	Log            *store.RecallLog
	ScopeLabel     string
	ChangeScopeURL string
	// NotebookEmpty distinguishes a notebook with no words at all from a
	// non-empty notebook whose selected scope happens to be empty.
	NotebookEmpty bool
}

// summaryData feeds the end-of-round summary.
type summaryData struct {
	Base
	Notebook       store.Notebook
	Known          int
	Unknown        int
	Total          int
	ScopeLabel     string
	RestartURL     string
	ChangeScopeURL string
}

func (s *Server) handleRecallSelect(w http.ResponseWriter, r *http.Request) {
	base, ok := s.pageData(w, r, "回忆单词", "recall")
	if !ok {
		return
	}
	_ = s.renderer.page(w, http.StatusOK, "recall_select", base)
}

// loadRecallNotebook validates {id} belongs to the current user.
func (s *Server) loadRecallNotebook(w http.ResponseWriter, r *http.Request) (store.Notebook, bool) {
	id, ok := s.pathID(w, r)
	if !ok {
		return store.Notebook{}, false
	}
	nb, err := s.store.NotebookByID(r.Context(), currentUser(r).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "生词本不存在。")
		return store.Notebook{}, false
	}
	if err != nil {
		s.fail(w, r, err)
		return store.Notebook{}, false
	}
	return nb, true
}

// pickWord selects the next card, excluding the current one when possible.
func (s *Server) pickWord(words []store.Word, excludeID int64) *store.Word {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	w, ok := recall.Pick(words, s.now().UTC(), excludeID, rng)
	if !ok {
		return nil
	}
	return &w
}

func (s *Server) handleRecallPage(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadRecallNotebook(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).ID
	state := s.recall.Begin(userID, nb.ID)
	words, err := s.store.ListRecallWords(r.Context(), nb.ID, state.Scope)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var word *store.Word
	if len(words) > 0 {
		word = s.pickWord(words, state.LastWordID)
		if word != nil {
			s.recall.SetLastWord(userID, word.ID)
			state = s.recall.Snapshot(userID)
		}
	}
	groups, err := s.store.ListWordGroups(r.Context(), nb.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	base, ok := s.pageData(w, r, nb.Name, "recall")
	if !ok {
		return
	}
	_ = s.renderer.page(w, http.StatusOK, "recall", recallView{
		Base:           base,
		Notebook:       nb,
		Word:           word,
		State:          state,
		ScopeLabel:     scopeLabel(state.Scope, groups),
		ChangeScopeURL: changeScopeURL(nb.ID, state.Scope),
		NotebookEmpty:  nb.WordCount == 0,
	})
}

func (s *Server) handleRecallAnswer(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadRecallNotebook(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	wordID, ok := parseID(r.PostForm.Get("word_id"))
	if !ok {
		http.Error(w, "缺少单词", http.StatusBadRequest)
		return
	}
	known := r.PostForm.Get("known") == "1" || r.PostForm.Get("known") == "true"

	userID := currentUser(r).ID
	state, err := s.recall.Answer(userID, nb.ID, wordID, known)
	if errors.Is(err, recall.ErrAlreadyAnswered) {
		http.Error(w, "这道题已经答过啦", http.StatusConflict)
		return
	}
	if errors.Is(err, recall.ErrWrongNotebook) {
		http.Error(w, "回忆会话已失效，请重新选本。", http.StatusConflict)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}

	word, log, err := s.store.AnswerWord(r.Context(), nb.ID, wordID, known, s.now().UTC())
	if err != nil {
		s.recall.UndoAnswer(userID, wordID)
		s.fail(w, r, err)
		return
	}
	_ = s.renderer.fragment(w, http.StatusOK, "recall_answer", recallView{
		Notebook: nb,
		Word:     &word,
		State:    state,
		Log:      &log,
	})
}

func (s *Server) handleRecallNext(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadRecallNotebook(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).ID
	state := s.recall.Begin(userID, nb.ID)
	words, err := s.store.ListRecallWords(r.Context(), nb.ID, state.Scope)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var word *store.Word
	if len(words) > 0 {
		word = s.pickWord(words, state.LastWordID)
		if word != nil {
			s.recall.SetLastWord(userID, word.ID)
			state = s.recall.Snapshot(userID)
		}
	}
	_ = s.renderer.fragment(w, http.StatusOK, "recall_card", recallView{
		Notebook:       nb,
		Word:           word,
		State:          state,
		ChangeScopeURL: changeScopeURL(nb.ID, state.Scope),
		NotebookEmpty:  nb.WordCount == 0,
	})
}

func (s *Server) handleRecallEnd(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadRecallNotebook(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).ID
	state := s.recall.Snapshot(userID)
	s.recall.Reset(userID)
	redirect := "/recall/" + strconv.FormatInt(nb.ID, 10) + "/summary?known=" +
		strconv.Itoa(state.Known) + "&unknown=" + strconv.Itoa(state.Unknown) +
		"&" + scopeQuery(state.Scope).Encode()
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func parseCount(raw string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 || n > 1000000 {
		return 0, false
	}
	return n, true
}

func (s *Server) handleRecallSummary(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadRecallNotebook(w, r)
	if !ok {
		return
	}
	known, okKnown := parseCount(r.URL.Query().Get("known"))
	unknown, okUnknown := parseCount(r.URL.Query().Get("unknown"))
	if !okKnown || !okUnknown {
		s.renderError(w, r, http.StatusBadRequest, "统计数字不正确。")
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
	// scopeLabel only needs group names for a group scope.
	var groups []store.WordGroup
	if scope.Kind == store.ScopeGroups {
		groups, err = s.store.ListWordGroups(r.Context(), nb.ID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}
	base, ok := s.pageData(w, r, "本轮小结", "recall")
	if !ok {
		return
	}
	_ = s.renderer.page(w, http.StatusOK, "summary", summaryData{
		Base:           base,
		Notebook:       nb,
		Known:          known,
		Unknown:        unknown,
		Total:          known + unknown,
		ScopeLabel:     scopeLabel(scope, groups),
		RestartURL:     "/recall/" + strconv.FormatInt(nb.ID, 10) + "/restart?" + scopeQuery(scope).Encode(),
		ChangeScopeURL: changeScopeURL(nb.ID, scope),
	})
}

func (s *Server) handleRecallRestart(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadRecallNotebook(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).ID
	scope, err := parseRecallScope(r.URL.Query())
	if errors.Is(err, store.ErrInvalidScope) {
		s.renderError(w, r, http.StatusBadRequest, "范围参数不对。")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	startURL := "/recall/" + strconv.FormatInt(nb.ID, 10) + "/start?" + scopeQuery(scope).Encode()
	groupsOK, err := s.scopeGroupsExist(r.Context(), nb.ID, scope)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !groupsOK {
		s.recall.Reset(userID)
		setFlash(w, "这个范围里还没有单词，先换个范围吧。")
		http.Redirect(w, r, startURL, http.StatusSeeOther)
		return
	}
	words, err := s.store.ListRecallWords(r.Context(), nb.ID, scope)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(words) == 0 {
		s.recall.Reset(userID)
		setFlash(w, "这个范围里还没有单词，先换个范围吧。")
		http.Redirect(w, r, startURL, http.StatusSeeOther)
		return
	}
	s.recall.Start(userID, nb.ID, scope)
	http.Redirect(w, r, "/recall/"+strconv.FormatInt(nb.ID, 10), http.StatusSeeOther)
}
