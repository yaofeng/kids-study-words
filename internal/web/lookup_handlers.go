package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yaofeng/kids-study-words/internal/dict"
	"github.com/yaofeng/kids-study-words/internal/store"
)

// Dictionary is the subset of the offline dictionary the web layer needs.
type Dictionary interface {
	SearchEnglish(ctx context.Context, q string, limit int) ([]dict.Entry, error)
	SearchChinese(ctx context.Context, q string, limit int) ([]dict.Entry, error)
	Lookup(ctx context.Context, word string) (dict.Entry, error)
	Examples(ctx context.Context, word, exchange string, limit int) ([]dict.Sentence, error)
}

// lookupData feeds the lookup page and its results fragment.
type lookupData struct {
	Base
	Mode    string
	Query   string
	Results []dict.Entry
	Hint    string
}

// examplesData feeds the examples fragment.
type examplesData struct {
	Sentences []store.Example
	Message   string
}

func normalizeMode(mode string) string {
	if mode == "zh" {
		return "zh"
	}
	return "en"
}

func (s *Server) searchDict(ctx context.Context, mode, q string) ([]dict.Entry, error) {
	if s.dict == nil || strings.TrimSpace(q) == "" {
		return nil, nil
	}
	if mode == "zh" {
		return s.dict.SearchChinese(ctx, q, 30)
	}
	return s.dict.SearchEnglish(ctx, q, 20)
}

func (s *Server) handleLookupPage(w http.ResponseWriter, r *http.Request) {
	mode := normalizeMode(r.URL.Query().Get("mode"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	base, ok := s.pageData(w, r, "查词", "lookup")
	if !ok {
		return
	}
	data := lookupData{Base: base, Mode: mode, Query: q}
	if q != "" {
		results, err := s.searchDict(r.Context(), mode, q)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		data.Results = results
	}
	if err := s.renderer.page(w, http.StatusOK, "lookup", data); err != nil {
		s.fail(w, r, err)
	}
}

func (s *Server) handleLookupResults(w http.ResponseWriter, r *http.Request) {
	mode := normalizeMode(r.URL.Query().Get("mode"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	base, ok := s.pageData(w, r, "查词", "lookup")
	if !ok {
		return
	}
	results, err := s.searchDict(r.Context(), mode, q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := lookupData{Base: base, Mode: mode, Query: q, Results: results}
	_ = s.renderer.fragment(w, http.StatusOK, "lookup_results", data)
}

func (s *Server) handleLookupExamples(w http.ResponseWriter, r *http.Request) {
	word := strings.TrimSpace(r.URL.Query().Get("word"))
	data := examplesData{Message: "暂无例句"}
	if word != "" && s.dict != nil {
		entry, err := s.dict.Lookup(r.Context(), word)
		if err == nil {
			data.Sentences = s.examplesFor(r.Context(), entry)
		} else if !errors.Is(err, dict.ErrNotFound) {
			data.Message = "例句暂时找不到了。"
		}
	}
	_ = s.renderer.fragment(w, http.StatusOK, "examples", data)
}

// examplesFor finds example sentences for a lookup: cached AI output first,
// then local pairs, then (only when local is thin) a fresh AI request.
func (s *Server) examplesFor(ctx context.Context, entry dict.Entry) []store.Example {
	if cached, ok, err := s.store.CachedExamples(ctx, entry.Word); err == nil && ok && len(cached) > 0 {
		return cached
	}
	local := s.localExamples(ctx, entry, 2)
	if len(local) >= 2 || s.ai == nil || !s.ai.Enabled() {
		return local
	}

	forms := dict.Forms(entry.Word, entry.Exchange)
	generated, err := s.ai.Examples(ctx, entry.Word, entry.Translation, forms)
	if err != nil || len(generated) == 0 {
		return local
	}
	examples := make([]store.Example, 0, len(generated))
	for _, ex := range generated {
		examples = append(examples, store.Example{EN: ex.EN, ZH: ex.ZH, Source: "ai"})
	}
	if err := s.store.SaveExamples(ctx, entry.Word, s.ai.Model(), examples); err != nil {
		slog.Warn("cache AI examples", "word", entry.Word, "err", err)
	}
	return examples
}

// snapshotExamples copies cached or local examples into a newly added word. It
// never calls the AI service, so adding a word stays fast.
func (s *Server) snapshotExamples(ctx context.Context, entry dict.Entry) []store.Example {
	if cached, ok, err := s.store.CachedExamples(ctx, entry.Word); err == nil && ok && len(cached) > 0 {
		return cached
	}
	return s.localExamples(ctx, entry, 2)
}

// localExamples converts up to limit local sentences into stored examples.
func (s *Server) localExamples(ctx context.Context, entry dict.Entry, limit int) []store.Example {
	if s.dict == nil {
		return nil
	}
	sentences, err := s.dict.Examples(ctx, entry.Word, entry.Exchange, limit)
	if err != nil {
		return nil
	}
	examples := make([]store.Example, 0, len(sentences))
	for _, sent := range sentences {
		examples = append(examples, store.Example{EN: sent.EN, ZH: sent.ZH, Source: sent.Source})
	}
	return examples
}

// safeNext only allows same-site absolute paths.
func safeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.ContainsAny(next, "\r\n") {
		return next
	}
	return "/lookup"
}

func (s *Server) handleAddWord(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	next := safeNext(r.PostForm.Get("next"))
	word := strings.TrimSpace(r.PostForm.Get("word"))

	notebookID, ok := parseID(r.PostForm.Get("notebook_id"))
	if !ok {
		setFlash(w, "请选择一个生词本。")
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	nb, err := s.store.NotebookByID(r.Context(), currentUser(r).ID, notebookID)
	if errors.Is(err, store.ErrNotFound) {
		setFlash(w, "生词本不存在。")
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.dict == nil {
		setFlash(w, "词典还没有准备好。")
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}

	entry, err := s.dict.Lookup(r.Context(), word)
	if errors.Is(err, dict.ErrNotFound) {
		setFlash(w, fmt.Sprintf("词典里没有找到「%s」。", word))
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}

	_, err = s.store.AddWord(r.Context(), nb.ID, store.Word{
		Word:        entry.Word,
		Phonetic:    entry.Phonetic,
		Translation: entry.Translation,
		Definition:  entry.Definition,
		Examples:    s.snapshotExamples(r.Context(), entry),
	})
	switch {
	case errors.Is(err, store.ErrDuplicate):
		setFlash(w, fmt.Sprintf("「%s」已在《%s》中，没有重复加入。", entry.Word, nb.Name))
	case err != nil:
		s.fail(w, r, err)
		return
	default:
		setFlash(w, fmt.Sprintf("已加入《%s》。", nb.Name))
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}
