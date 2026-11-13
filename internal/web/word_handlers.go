package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yaofeng/kids-study-words/internal/store"
)

const maxExamples = 5

// notebookDetailData feeds web/templates/notebook.html.
type notebookDetailData struct {
	Base
	Notebook store.Notebook
	Words    []store.Word
	Sort     string
}

// exampleRow is one editable sentence on the word edit page.
type exampleRow struct {
	N  int
	EN string
	ZH string
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
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).ID
	nb, err := s.store.NotebookByID(r.Context(), userID, id)
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "生词本不存在。")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sortKey := validSort(r.URL.Query().Get("sort"))
	words, err := s.store.ListWords(r.Context(), nb.ID, sortKey)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	base, ok := s.pageData(w, r, nb.Name, "notebooks")
	if !ok {
		return
	}
	data := notebookDetailData{Base: base, Notebook: nb, Words: words, Sort: sortKey}
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
		rows = append(rows, exampleRow{N: n, EN: en, ZH: zh})
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
		examples = append(examples, store.Example{EN: row.EN, ZH: row.ZH, Source: "manual"})
	}
	return examples, "", rows
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
