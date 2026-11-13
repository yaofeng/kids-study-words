package dict

import (
	"reflect"
	"testing"
)

func TestForms(t *testing.T) {
	got := Forms("go", "p:went/d:gone/i:going/3:goes/s:goes")
	want := []string{"go", "went", "gone", "going", "goes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFormsEmptyAndDeduped(t *testing.T) {
	if got := Forms("apple", ""); !reflect.DeepEqual(got, []string{"apple"}) {
		t.Fatalf("got %v", got)
	}
	if got := Forms("go", "p:go/d:/i:going"); !reflect.DeepEqual(got, []string{"go", "going"}) {
		t.Fatalf("got %v", got)
	}
}
