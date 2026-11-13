// Package recall chooses which word to practise and tracks session counts.
package recall

import (
	"math/rand"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// Weight scores how overdue a word is. Lower proficiency and longer gaps
// between reviews both increase the chance of being picked.
func Weight(proficiency int, lastReviewed *time.Time, now time.Time) float64 {
	if proficiency < 1 {
		proficiency = 1
	}
	if proficiency > 10 {
		proficiency = 10
	}
	difficulty := float64((11 - proficiency) * (11 - proficiency))

	var hours float64
	if lastReviewed == nil {
		hours = 30 * 24
	} else {
		hours = now.Sub(*lastReviewed).Hours()
		if hours < 0 {
			hours = 0
		}
	}
	recency := hours / 24
	if recency < 0.05 {
		recency = 0.05
	}
	if recency > 30 {
		recency = 30
	}
	return difficulty * recency
}

// Pick chooses a word by weighted random draw, avoiding excludeID when at
// least one other word is available. It returns false for an empty list.
func Pick(words []store.Word, now time.Time, excludeID int64, rng *rand.Rand) (store.Word, bool) {
	candidates := words
	if excludeID != 0 && len(words) > 1 {
		candidates = make([]store.Word, 0, len(words))
		for _, w := range words {
			if w.ID != excludeID {
				candidates = append(candidates, w)
			}
		}
	}
	if len(candidates) == 0 {
		return store.Word{}, false
	}

	total := 0.0
	weights := make([]float64, len(candidates))
	for i, w := range candidates {
		weights[i] = Weight(w.Proficiency, w.LastReviewedAt, now)
		total += weights[i]
	}

	roll := rng.Float64() * total
	acc := 0.0
	for i, w := range candidates {
		acc += weights[i]
		if acc > roll {
			return w, true
		}
	}
	return candidates[len(candidates)-1], true
}
