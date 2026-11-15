package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestRecallHidesMeaningUntilAnswer(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	body := tc.get("/recall/" + fmt.Sprint(tc.notebook.ID)).Body.String()
	if !strings.Contains(body, "apple") {
		t.Fatal("word missing")
	}
	if strings.Contains(body, "苹果") {
		t.Fatal("meaning must be hidden before answer")
	}
}

func TestKnownAnswerRevealsMeaningAndIncrements(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.get("/recall/" + fmt.Sprint(tc.notebook.ID))
	resp := tc.postForm(fmt.Sprintf("/recall/%d/answer", tc.notebook.ID), url.Values{
		"word_id": {fmt.Sprint(w.ID)}, "known": {"1"}, "csrf_token": {tc.csrf},
	})
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), "苹果") || !strings.Contains(resp.Body.String(), "熟练度") {
		t.Fatalf("code=%d body=%s", resp.Code, resp.Body.String())
	}
	got, err := tc.store.WordByID(context.Background(), tc.notebook.ID, w.ID)
	if err != nil || got.Proficiency != 6 {
		t.Fatalf("proficiency=%d err=%v", got.Proficiency, err)
	}
	logs, _ := tc.store.ListRecallLogs(context.Background(), tc.user.ID, nil, 0, 10)
	if len(logs) != 1 || !logs[0].Known {
		t.Fatalf("logs=%+v", logs)
	}
}

func TestDoubleAnswerIsRejected(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.get("/recall/" + fmt.Sprint(tc.notebook.ID))
	form := url.Values{"word_id": {fmt.Sprint(w.ID)}, "known": {"1"}, "csrf_token": {tc.csrf}}
	if resp := tc.postForm(fmt.Sprintf("/recall/%d/answer", tc.notebook.ID), form); resp.Code != 200 {
		t.Fatalf("first code=%d", resp.Code)
	}
	if resp := tc.postForm(fmt.Sprintf("/recall/%d/answer", tc.notebook.ID), form); resp.Code != http.StatusConflict {
		t.Fatalf("second code=%d body=%s", resp.Code, resp.Body.String())
	}
	got, _ := tc.store.WordByID(context.Background(), tc.notebook.ID, w.ID)
	if got.Proficiency != 6 {
		t.Fatalf("proficiency=%d", got.Proficiency)
	}
	logs, _ := tc.store.ListRecallLogs(context.Background(), tc.user.ID, nil, 0, 10)
	if len(logs) != 1 {
		t.Fatalf("logs=%d", len(logs))
	}
}

func TestEndSummaryCounts(t *testing.T) {
	tc := newAuthedTestClient(t)
	a := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	b := tc.seedWord(tc.notebook.ID, "pear", "n. 梨")
	tc.get("/recall/" + fmt.Sprint(tc.notebook.ID))
	// Count directly through the manager so random picking cannot change the
	// assertion.
	if _, err := tc.srv.recall.Answer(tc.user.ID, tc.notebook.ID, a.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := tc.srv.recall.Answer(tc.user.ID, tc.notebook.ID, b.ID, false); err != nil {
		t.Fatal(err)
	}
	resp := tc.postForm(fmt.Sprintf("/recall/%d/end", tc.notebook.ID), url.Values{})
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("code=%d", resp.Code)
	}
	loc := resp.Header().Get("Location")
	if !strings.Contains(loc, "known=1") || !strings.Contains(loc, "unknown=1") {
		t.Fatalf("location=%s", loc)
	}
	body := tc.get(loc).Body.String()
	if !strings.Contains(body, "认识 1") || !strings.Contains(body, "不认识 1") {
		t.Fatalf("body=%s", body)
	}
}

func TestNextShowsAnotherWord(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	tc.seedWord(tc.notebook.ID, "pear", "n. 梨")
	tc.get("/recall/" + fmt.Sprint(tc.notebook.ID))
	resp := tc.postForm(fmt.Sprintf("/recall/%d/next", tc.notebook.ID), url.Values{})
	if resp.Code != 200 {
		t.Fatalf("code=%d", resp.Code)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "apple") && !strings.Contains(body, "pear") {
		t.Fatalf("body=%s", body)
	}
	if strings.Contains(body, "苹果") || strings.Contains(body, "梨") {
		t.Fatalf("next card should hide meaning: %s", body)
	}
}

func TestRecallRejectsForeignNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)
	resp := tc.get("/recall/99999")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("code=%d", resp.Code)
	}
}

func TestRecallEmptyNotebook(t *testing.T) {
	tc := newAuthedTestClient(t)
	body := tc.get("/recall/" + fmt.Sprint(tc.notebook.ID)).Body.String()
	if !strings.Contains(body, "这个生词本还没有单词") || strings.Contains(body, "换个范围") {
		t.Fatalf("body=%s", body)
	}
}

func TestRecallSelectPage(t *testing.T) {
	tc := newAuthedTestClient(t)
	tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	body := tc.get("/recall").Body.String()
	if !strings.Contains(body, "默认生词本") || !strings.Contains(body, "开始回忆") {
		t.Fatalf("body=%s", body)
	}
}

func TestRecallHidesPhoneticUntilAnswer(t *testing.T) {
	tc := newAuthedTestClient(t)
	w, err := tc.store.AddWord(context.Background(), tc.notebook.ID, store.Word{
		Word: "apple", Phonetic: "/ˈæpl/", Translation: "n. 苹果",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := tc.get("/recall/" + fmt.Sprint(tc.notebook.ID)).Body.String()
	if strings.Contains(body, "/ˈæpl/") {
		t.Fatal("phonetic must stay hidden before the child answers")
	}
	resp := tc.postForm(fmt.Sprintf("/recall/%d/answer", tc.notebook.ID), url.Values{
		"word_id": {fmt.Sprint(w.ID)}, "known": {"1"}, "csrf_token": {tc.csrf},
	})
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "/ˈæpl/") {
		t.Fatalf("phonetic should appear after the answer: code=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRecallShowsTimerAndRoundCount(t *testing.T) {
	tc := newAuthedTestClient(t)
	w := tc.seedWord(tc.notebook.ID, "apple", "n. 苹果")
	body := tc.get("/recall/" + fmt.Sprint(tc.notebook.ID)).Body.String()
	if !strings.Contains(body, "data-round-timer") || !strings.Contains(body, "认识 0 / 0") {
		t.Fatalf("timer or count missing: %s", body)
	}
	m := regexp.MustCompile(`data-start="(\d+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("timer start missing: %s", body)
	}
	start := m[1]

	resp := tc.postForm(fmt.Sprintf("/recall/%d/answer", tc.notebook.ID), url.Values{
		"word_id": {fmt.Sprint(w.ID)}, "known": {"1"}, "csrf_token": {tc.csrf},
	})
	after := resp.Body.String()
	if !strings.Contains(after, "认识 1 / 1") {
		t.Fatalf("answer should count 1/1: %s", after)
	}
	if !strings.Contains(after, `data-start="`+start+`"`) {
		t.Fatalf("timer restarted after answering: %s", after)
	}

	next := tc.postForm(fmt.Sprintf("/recall/%d/next", tc.notebook.ID), url.Values{})
	if !strings.Contains(next.Body.String(), `data-start="`+start+`"`) {
		t.Fatalf("timer restarted on next card: %s", next.Body.String())
	}
}
