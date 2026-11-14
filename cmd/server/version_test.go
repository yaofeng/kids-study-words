package main

import (
	"testing"

	studywords "github.com/yaofeng/kids-study-words"
)

func TestVersionComesFromDotVersion(t *testing.T) {
	if version != studywords.Version {
		t.Fatalf("main.version = %q, want %q from .version", version, studywords.Version)
	}
}
