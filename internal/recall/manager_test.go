package recall

import (
	"errors"
	"testing"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestManagerDoubleAnswerRejected(t *testing.T) {
	m := NewManager()
	m.Begin(1, 10)
	if _, err := m.Answer(1, 10, 99, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Answer(1, 10, 99, false); !errors.Is(err, ErrAlreadyAnswered) {
		t.Fatalf("got %v", err)
	}
}

func TestManagerCountsAndNotebookSwitch(t *testing.T) {
	m := NewManager()
	st := m.Begin(1, 10)
	if st.NotebookID != 10 || st.Known != 0 || st.Unknown != 0 {
		t.Fatalf("state=%+v", st)
	}
	st, err := m.Answer(1, 10, 1, true)
	if err != nil || st.Known != 1 {
		t.Fatalf("state=%+v err=%v", st, err)
	}
	st, err = m.Answer(1, 10, 2, false)
	if err != nil || st.Unknown != 1 || st.AnsweredWordID != 2 {
		t.Fatalf("state=%+v err=%v", st, err)
	}
	if _, err := m.Answer(1, 11, 3, true); !errors.Is(err, ErrWrongNotebook) {
		t.Fatalf("expected ErrWrongNotebook, got %v", err)
	}
	st = m.Begin(1, 11)
	if st.NotebookID != 11 || st.Known != 0 || st.Unknown != 0 {
		t.Fatalf("switch should reset: %+v", st)
	}
}

func TestManagerUndoAndLastWord(t *testing.T) {
	m := NewManager()
	m.Begin(1, 10)
	m.SetLastWord(1, 42)
	if st := m.Snapshot(1); st.LastWordID != 42 {
		t.Fatalf("state=%+v", st)
	}
	if _, err := m.Answer(1, 10, 42, true); err != nil {
		t.Fatal(err)
	}
	m.UndoAnswer(1, 42)
	st := m.Snapshot(1)
	if st.Known != 0 || st.AnsweredWordID != 0 {
		t.Fatalf("state=%+v", st)
	}
	if _, err := m.Answer(1, 10, 42, true); err != nil {
		t.Fatalf("re-answer after undo should work: %v", err)
	}
	m.Reset(1)
	if st := m.Snapshot(1); st.NotebookID != 0 || st.Known != 0 {
		t.Fatalf("state=%+v", st)
	}
}

func TestManagerRoundStartedAt(t *testing.T) {
	m := NewManager()
	st := m.Begin(1, 10)
	if st.StartedAt.IsZero() {
		t.Fatal("new round should record its start time")
	}
	start := st.StartedAt
	time.Sleep(2 * time.Millisecond)
	if again := m.Begin(1, 10); !again.StartedAt.Equal(start) {
		t.Fatalf("re-entering the same round restarted the timer: %v != %v", again.StartedAt, start)
	}
	if switched := m.Begin(1, 11); !switched.StartedAt.After(start) {
		t.Fatalf("switching notebooks should start a new round: %v", switched.StartedAt)
	}
}

func TestManagerStartSetsScopeAndResets(t *testing.T) {
	m := NewManager()
	scope := store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{2, 1}}
	s := m.Start(1, 10, scope)
	if s.NotebookID != 10 || s.Scope.Kind != store.ScopeGroups || len(s.Scope.GroupIDs) != 2 {
		t.Fatalf("state=%+v", s)
	}
	if _, err := m.Answer(1, 10, 5, true); err != nil {
		t.Fatal(err)
	}
	s = m.Start(1, 10, store.RecallScope{Kind: store.ScopeProficiency, MaxProficiency: 6})
	if s.Known != 0 || s.Unknown != 0 || s.Scope.Kind != store.ScopeProficiency || s.Scope.MaxProficiency != 6 {
		t.Fatalf("restart should reset: %+v", s)
	}
}

func TestManagerBeginKeepsActiveScope(t *testing.T) {
	m := NewManager()
	m.Start(1, 10, store.RecallScope{Kind: store.ScopeGroups, GroupIDs: []int64{3}})
	s := m.Begin(1, 10)
	if s.Scope.Kind != store.ScopeGroups || len(s.Scope.GroupIDs) != 1 {
		t.Fatalf("begin should keep scope: %+v", s)
	}
	s = m.Begin(1, 11)
	if s.Scope.Kind != store.ScopeAll || s.NotebookID != 11 {
		t.Fatalf("new notebook should default to all: %+v", s)
	}
}

func TestManagerScopeGroupsAreCopied(t *testing.T) {
	m := NewManager()
	ids := []int64{4, 5}
	m.Start(1, 10, store.RecallScope{Kind: store.ScopeGroups, GroupIDs: ids})
	ids[0] = 99
	s := m.Snapshot(1)
	s.Scope.GroupIDs[0] = 100
	if got := m.Snapshot(1).Scope.GroupIDs; len(got) != 2 || got[0] != 4 {
		t.Fatalf("state should not share the caller's slice: %v", got)
	}
}
