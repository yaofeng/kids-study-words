package recall

import (
	"math/rand"
	"testing"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

func TestWeightMonotonic(t *testing.T) {
	now := time.Now()
	week := now.Add(-7 * 24 * time.Hour)
	if Weight(1, &week, now) <= Weight(10, &week, now) {
		t.Fatal("lower proficiency must weigh more")
	}
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-time.Minute)
	if Weight(5, &old, now) <= Weight(5, &recent, now) {
		t.Fatal("older must weigh more")
	}
	if Weight(10, &recent, now) <= 0 {
		t.Fatal("weight must stay positive")
	}
	if got, want := Weight(5, nil, now), Weight(5, &old, now); got != want {
		t.Fatalf("never-reviewed weight = %v, want %v (30 days)", got, want)
	}
}

func TestPickExcludesLastWordWhenPossible(t *testing.T) {
	now := time.Now()
	words := []store.Word{{ID: 1}, {ID: 2}}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		w, ok := Pick(words, now, 1, rng)
		if !ok || w.ID != 2 {
			t.Fatalf("picked %+v ok=%v", w, ok)
		}
	}
	if _, ok := Pick([]store.Word{{ID: 1}}, now, 1, rng); !ok {
		t.Fatal("single word must still be pickable")
	}
	if _, ok := Pick(nil, now, 0, rng); ok {
		t.Fatal("empty list must return false")
	}
}

func TestPickPrefersWeakWords(t *testing.T) {
	now := time.Now()
	old := now.Add(-60 * 24 * time.Hour)
	words := []store.Word{
		{ID: 1, Proficiency: 10, LastReviewedAt: &old},
		{ID: 2, Proficiency: 1, LastReviewedAt: &old},
	}
	rng := rand.New(rand.NewSource(7))
	counts := map[int64]int{}
	for i := 0; i < 1000; i++ {
		w, ok := Pick(words, now, 0, rng)
		if !ok {
			t.Fatal("expected a pick")
		}
		counts[w.ID]++
	}
	if counts[2] < 5*counts[1] {
		t.Fatalf("weak word should dominate: %+v", counts)
	}
}
