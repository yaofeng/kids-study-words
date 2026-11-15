package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func (tc *testClient) postImport(t *testing.T, doc []byte, mode, confirm string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("mode", mode); err != nil {
		t.Fatal(err)
	}
	if confirm != "" {
		if err := mw.WriteField("confirm", confirm); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.WriteField("csrf_token", tc.csrf); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "study-words.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(doc); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return tc.do(req)
}

func sampleExport(t *testing.T, notebooks ...string) []byte {
	t.Helper()
	f := store.ExportFile{
		Format:     "study-words",
		Version:    2,
		ExportedAt: time.Now().UTC(),
		Notebooks:  []store.ExportNotebook{},
	}
	for i, name := range notebooks {
		f.Notebooks = append(f.Notebooks, store.ExportNotebook{
			Name:      name,
			IsDefault: i == 0,
			Words:     []store.ExportWord{{Word: "apple", Translation: "n. 苹果", Proficiency: 6}},
		})
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestImportV2GroupsRestoresGroupsAndSummarizes(t *testing.T) {
	tc := newAuthedTestClient(t)
	doc := store.ExportFile{
		Format:  "study-words",
		Version: 2,
		Notebooks: []store.ExportNotebook{{
			Name:   "动物",
			Words:  []store.ExportWord{{Word: "apple", Proficiency: 5}, {Word: "pear", Proficiency: 5}},
			Groups: []store.ExportGroup{{Name: "水果", Words: []string{"apple"}}},
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	rec := tc.postImport(t, body, "merge", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}

	settings := tc.get("/settings").Body.String()
	if !strings.Contains(settings, "新建 1 个分组") || !strings.Contains(settings, "恢复 1 个分组词") {
		t.Fatalf("import summary missing groups: %s", settings)
	}

	rec = tc.get("/export")
	if rec.Code != http.StatusOK {
		t.Fatalf("export code=%d", rec.Code)
	}
	var f store.ExportFile
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	var animal *store.ExportNotebook
	for i := range f.Notebooks {
		if f.Notebooks[i].Name == "动物" {
			animal = &f.Notebooks[i]
		}
	}
	if animal == nil || len(animal.Groups) != 1 || animal.Groups[0].Name != "水果" ||
		!slices.Equal(animal.Groups[0].Words, []string{"apple"}) {
		t.Fatalf("exported groups=%+v", animal)
	}
}

func TestSettingsPageShowsDataSection(t *testing.T) {
	tc := newAuthedTestClient(t)
	rec := tc.get("/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"设置", "导出", "导入", "/export", "/import"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
}

func TestExportAllReturnsJSONAttachment(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")

	rec := tc.get("/export")
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content-type=%q", got)
	}
	want := "attachment; filename=\"study-words-" + time.Now().UTC().Format("20060102") + ".json\""
	if got := rec.Header().Get("Content-Disposition"); got != want {
		t.Fatalf("content-disposition=%q want %q", got, want)
	}
	var f store.ExportFile
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if f.Format != "study-words" || len(f.Notebooks) != 1 || len(f.Notebooks[0].Words) != 1 {
		t.Fatalf("export=%+v", f)
	}
}

func TestExportSingleNotebookReturnsOnlyThatOne(t *testing.T) {
	tc := newAuthedTestClient(t)
	other, err := tc.store.CreateNotebook(context.Background(), tc.user.ID, "动物")
	if err != nil {
		t.Fatal(err)
	}
	tc.seedWord(other.ID, "cat", "n. 猫")

	rec := tc.get("/export?notebook=" + strconv.FormatInt(other.ID, 10))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	var f store.ExportFile
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Notebooks) != 1 || f.Notebooks[0].Name != "动物" {
		t.Fatalf("export=%+v", f)
	}

	if rec := tc.get("/export?notebook=9999"); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign notebook should 404, got %d", rec.Code)
	}
	if rec := tc.get("/export?notebook=abc"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad notebook should 400, got %d", rec.Code)
	}
}

func TestImportMergeAddsMissingNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)

	rec := tc.postImport(t, sampleExport(t, "动物"), "merge", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings" {
		t.Fatalf("location=%q", loc)
	}
	nbs, err := tc.store.ListNotebooks(context.Background(), tc.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nbs) != 2 {
		t.Fatalf("notebooks=%+v", nbs)
	}
	def, err := tc.store.DefaultNotebook(context.Background(), tc.user.ID)
	if err != nil || def.Name != tc.notebook.Name {
		t.Fatalf("merge must keep the current default: %+v %v", def, err)
	}
}

func TestImportOverwriteRequiresConfirm(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "old", "旧的")

	rec := tc.postImport(t, sampleExport(t, "全新"), "overwrite", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", rec.Code)
	}
	words, err := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if err != nil || len(words) != 1 {
		t.Fatalf("data changed without confirm: %+v %v", words, err)
	}

	rec = tc.postImport(t, sampleExport(t, "全新"), "overwrite", "yes")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	nbs, err := tc.store.ListNotebooks(context.Background(), tc.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nbs) != 1 || nbs[0].Name != "全新" {
		t.Fatalf("notebooks=%+v", nbs)
	}
	words, err = tc.store.ListWords(context.Background(), nbs[0].ID, "added")
	if err != nil || len(words) != 1 || words[0].Word != "apple" {
		t.Fatalf("words=%+v err=%v", words, err)
	}
}

func TestImportRejectsInvalidFile(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "old", "旧的")

	rec := tc.postImport(t, []byte("{not json"), "merge", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "导入") {
		t.Fatalf("expected settings page with import error, got %s", rec.Body)
	}

	rec = tc.postImport(t, sampleExport(t, "x"), "sideways", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown mode should 400, got %d", rec.Code)
	}
	words, err := tc.store.ListWords(context.Background(), tc.notebook.ID, "added")
	if err != nil || len(words) != 1 {
		t.Fatalf("data changed by failed imports: %+v %v", words, err)
	}
}

func TestImportRejectsFileOverLimit(t *testing.T) {
	tc := newAuthedTestClient(t)
	big := bytes.Repeat([]byte(" "), 11<<20)
	rec := tc.postImport(t, big, "merge", "")
	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestSettingsOverwriteConfirmShowsCounts(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.seedWord(tc.notebook.ID, "banana", "n. 香蕉")
	body := tc.get("/settings").Body.String()
	if !strings.Contains(body, "1 个生词本") || !strings.Contains(body, "2 个单词") {
		t.Fatalf("overwrite confirmation must say what will be deleted: %s", body)
	}
}
