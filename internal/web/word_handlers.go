package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yaofeng/kids-study-words/internal/store"
)

const maxExamples = 5

// wordCard feeds the shared word_card partial. Word is a named field, not an
// embedded store.Word: embedding would create an outer field also called Word,
// shadowing the word's own Word string in templates. DeleteURL is empty on
// read-only pages, which hides the swipe-to-delete form; Groups is empty when
// the card must not show notebook group badges.
type wordCard struct {
	CSRF      string
	DeleteURL string
	Word      store.Word
	Groups    []store.WordGroup
}

// notebookDetailData feeds web/templates/notebook.html.
type notebookDetailData struct {
	Base
	Notebook   store.Notebook
	Words      []wordCard
	Groups     []store.WordGroup
	Sort       string
	GroupError string
}

// exampleRow is one editable sentence on the word edit page.
type exampleRow struct {
	N      int
	EN     string
	ZH     string
	Source string
}

// wordEditData feeds web/templates/word_edit.html.
type wordEditData struct {
	Base
	Notebook store.Notebook
	Word     store.Word
	Rows     []exampleRow
}

func validSort(s string) string {
	switch s {
	case "proficiency", "reviewed":
		return s
	default:
		return "added"
	}
}

func (s *Server) handleNotebookDetail(w http.ResponseWriter, r *http.Request) {
	nb, ok := s.loadNotebook(w, r)
	if !ok {
		return
	}
	s.renderNotebookDetail(w, r, nb, r.URL.Query().Get("sort"), "")
}

// renderNotebookDetail renders the notebook page, optionally with a group error.
func (s *Server) renderNotebookDetail(w http.ResponseWriter, r *http.Request, nb store.Notebook, sortKey, groupErr string) {
	sortKey = validSort(sortKey)
	base, ok := s.pageData(w, r, nb.Name, "notebooks")
	if !ok {
		return
	}
	words, err := s.store.ListWords(r.Context(), nb.ID, sortKey)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	groups, err := s.store.ListWordGroups(r.Context(), nb.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	members, err := s.store.GroupMemberships(r.Context(), nb.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]wordCard, len(words))
	for i, w := range words {
		out[i] = wordCard{
			CSRF:      base.CSRF,
			DeleteURL: "/notebooks/" + itoa(nb.ID) + "/words/" + itoa(w.ID) + "/delete",
			Word:      w,
			Groups:    members[w.ID],
		}
	}
	data := notebookDetailData{
		Base:       base,
		Notebook:   nb,
		Words:      out,
		Groups:     groups,
		Sort:       sortKey,
		GroupError: groupErr,
	}
	_ = s.renderer.page(w, http.StatusOK, "notebook", data)
}

func (s *Server) loadWordPage(w http.ResponseWriter, r *http.Request) (store.Notebook, store.Word, bool) {
	nbID, ok := s.pathID(w, r)
	if !ok {
		return store.Notebook{}, store.Word{}, false
	}
	wordID, err := strconv.ParseInt(r.PathValue("wordID"), 10, 64)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "单词不存在。")
		return store.Notebook{}, store.Word{}, false
	}
	nb, err := s.store.NotebookByID(r.Context(), currentUser(r).ID, nbID)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "生词本不存在。")
		return store.Notebook{}, store.Word{}, false
	}
	if err != nil {
		s.fail(w, r, err)
		return store.Notebook{}, store.Word{}, false
	}
	word, err := s.store.WordByID(r.Context(), nb.ID, wordID)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "单词不存在。")
		return store.Notebook{}, store.Word{}, false
	}
	if err != nil {
		s.fail(w, r, err)
		return store.Notebook{}, store.Word{}, false
	}
	return nb, word, true
}

func (s *Server) handleWordEditPage(w http.ResponseWriter, r *http.Request) {
	nb, word, ok := s.loadWordPage(w, r)
	if !ok {
		return
	}
	base, ok := s.pageData(w, r, "编辑单词", "notebooks")
	if !ok {
		return
	}
	_ = s.renderer.page(w, http.StatusOK, "word_edit", wordEditData{
		Base:     base,
		Notebook: nb,
		Word:     word,
		Rows:     exampleRows(word.Examples, len(word.Examples)+1),
	})
}

func (s *Server) handleWordEditPost(w http.ResponseWriter, r *http.Request) {
	nb, word, ok := s.loadWordPage(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "表单格式不对。")
		return
	}
	translation := strings.TrimSpace(r.PostForm.Get("translation"))

	renderForm := func(msg string, rows []exampleRow) {
		base, ok := s.pageData(w, r, "编辑单词", "notebooks")
		if !ok {
			return
		}
		base.Error = msg
		_ = s.renderer.page(w, http.StatusOK, "word_edit", wordEditData{
			Base:     base,
			Notebook: nb,
			Word:     word,
			Rows:     rows,
		})
	}

	if translation == "" {
		renderForm("释义不能为空。", collectedRows(r))
		return
	}
	examples, msg, rows := parseExampleRows(r)
	if msg != "" {
		renderForm(msg, rows)
		return
	}
	if err := s.store.UpdateWord(r.Context(), nb.ID, word.ID, translation, examples); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "单词不存在。")
			return
		}
		s.fail(w, r, err)
		return
	}
	setFlash(w, "单词已更新。")
	http.Redirect(w, r, "/notebooks/"+itoa(nb.ID), http.StatusSeeOther)
}

func (s *Server) handleWordDelete(w http.ResponseWriter, r *http.Request) {
	nb, word, ok := s.loadWordPage(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteWord(r.Context(), nb.ID, word.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "单词不存在。")
			return
		}
		s.fail(w, r, err)
		return
	}
	setFlash(w, "单词已删除。")
	http.Redirect(w, r, "/notebooks/"+itoa(nb.ID), http.StatusSeeOther)
}

// exampleRows turns stored examples into editable rows, padded with blanks.
func exampleRows(examples []store.Example, want int) []exampleRow {
	if want < 1 {
		want = 1
	}
	if want > maxExamples {
		want = maxExamples
	}
	rows := make([]exampleRow, want)
	for i := range rows {
		rows[i].N = i + 1
		if i < len(examples) {
			rows[i].EN = examples[i].EN
			rows[i].ZH = examples[i].ZH
			rows[i].Source = examples[i].Source
		}
	}
	return rows
}

// collectedRows rebuilds rows from submitted values without validation.
func collectedRows(r *http.Request) []exampleRow {
	var rows []exampleRow
	for n := 1; n <= maxExamples; n++ {
		en := strings.TrimSpace(r.PostForm.Get("example_en_" + itoa(int64(n))))
		zh := strings.TrimSpace(r.PostForm.Get("example_zh_" + itoa(int64(n))))
		if en == "" && zh == "" {
			continue
		}
		source := strings.TrimSpace(r.PostForm.Get("example_source_" + itoa(int64(n))))
		if source == "" {
			source = "manual"
		}
		rows = append(rows, exampleRow{N: n, EN: en, ZH: zh, Source: source})
	}
	if len(rows) == 0 {
		rows = []exampleRow{{N: 1}}
	}
	return rows
}

// parseExampleRows validates EN/ZH pairs and any error message for the form.
func parseExampleRows(r *http.Request) ([]store.Example, string, []exampleRow) {
	var examples []store.Example
	rows := collectedRows(r)
	for _, row := range rows {
		switch {
		case row.EN == "" && row.ZH == "":
			continue
		case row.EN == "" || row.ZH == "":
			return nil, "例句的英文和中文要同时填写。", rows
		}
		examples = append(examples, store.Example{EN: row.EN, ZH: row.ZH, Source: row.Source})
	}
	return examples, "", rows
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
