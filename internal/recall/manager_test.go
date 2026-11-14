package recall

import (
	"errors"
	"testing"
	"time"
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
