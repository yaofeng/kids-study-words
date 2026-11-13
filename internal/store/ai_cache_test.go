package store

import (
	"context"
	"testing"
)

func TestSaveAndLoadCachedExamples(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if ex, ok, err := st.CachedExamples(ctx, "go"); err != nil || ok || ex != nil {
		t.Fatalf("empty cache: %+v %v %v", ex, ok, err)
	}
	if err := st.SaveExamples(ctx, "Go", "model-a", []Example{{EN: "I go home.", ZH: "我回家。", Source: "ai"}}); err != nil {
		t.Fatal(err)
	}
	ex, ok, err := st.CachedExamples(ctx, "go")
	if err != nil || !ok || len(ex) != 1 || ex[0].EN != "I go home." || ex[0].Source != "ai" {
		t.Fatalf("cache hit: %+v %v %v", ex, ok, err)
	}

	if err := st.SaveExamples(ctx, "go", "model-b", []Example{{EN: "Go now.", ZH: "现在走。", Source: "ai"}}); err != nil {
		t.Fatal(err)
	}
	ex, ok, err = st.CachedExamples(ctx, "go")
	if err != nil || !ok || len(ex) != 1 || ex[0].EN != "Go now." {
		t.Fatalf("upsert: %+v %v %v", ex, ok, err)
	}
}
