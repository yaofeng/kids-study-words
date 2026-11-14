package recall

import (
	"errors"
	"sync"
	"time"
)

// Errors reported by Manager.
var (
	ErrAlreadyAnswered = errors.New("word already answered in this round")
	ErrWrongNotebook   = errors.New("recall session belongs to another notebook")
)

// State is a snapshot of one user's current recall round.
type State struct {
	NotebookID     int64
	Known          int
	Unknown        int
	StartedAt      time.Time
	LastWordID     int64
	AnsweredWordID int64

	lastKnown bool
}

// Total is the number of cards answered this round.
func (s State) Total() int {
	return s.Known + s.Unknown
}

// Manager keeps in-memory round state; persistence lives in recall_logs.
type Manager struct {
	mu     sync.Mutex
	states map[int64]*State
}

// NewManager creates an empty manager.
func NewManager() *Manager {
	return &Manager{states: make(map[int64]*State)}
}

func cloneState(s *State) State {
	if s == nil {
		return State{}
	}
	return *s
}

// Begin starts (or keeps) a round for a notebook.
func (m *Manager) Begin(userID, notebookID int64) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.states[userID]
	if s == nil || s.NotebookID != notebookID {
		s = &State{NotebookID: notebookID, StartedAt: time.Now()}
		m.states[userID] = s
	}
	return *s
}

// Snapshot returns the current round without modifying it.
func (m *Manager) Snapshot(userID int64) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneState(m.states[userID])
}

// SetLastWord records and returns a fresh card, clearing the answered guard.
func (m *Manager) SetLastWord(userID, wordID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.states[userID]
	if s == nil {
		s = &State{StartedAt: time.Now()}
		m.states[userID] = s
	}
	s.LastWordID = wordID
	s.AnsweredWordID = 0
}

// Answer counts an answer, rejecting a second answer for the same card.
func (m *Manager) Answer(userID, notebookID, wordID int64, known bool) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.states[userID]
	if s == nil || s.NotebookID != notebookID {
		return State{}, ErrWrongNotebook
	}
	if s.AnsweredWordID == wordID {
		return *s, ErrAlreadyAnswered
	}
	if known {
		s.Known++
	} else {
		s.Unknown++
	}
	s.AnsweredWordID = wordID
	s.lastKnown = known
	return *s, nil
}

// UndoAnswer reverses the count for wordID, used when the answer transaction
// fails after the in-memory count was applied.
func (m *Manager) UndoAnswer(userID, wordID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.states[userID]
	if s == nil || s.AnsweredWordID != wordID {
		return
	}
	if s.lastKnown {
		if s.Known > 0 {
			s.Known--
		}
	} else if s.Unknown > 0 {
		s.Unknown--
	}
	s.AnsweredWordID = 0
}

// Reset drops the round state for a user.
func (m *Manager) Reset(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, userID)
}
