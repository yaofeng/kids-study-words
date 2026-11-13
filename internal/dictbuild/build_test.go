package dictbuild_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yaofeng/kids-study-words/internal/dict"
	"github.com/yaofeng/kids-study-words/internal/dictbuild"
)

func TestBuildAndSearch(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "ecdict.csv")
	content := "word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange,detail,audio\n" +
		"apple,/ˈæpl/,a round fruit,n. 苹果,n,3,1,zk,100,200,,,\n" +
		"go,/ɡəʊ/,to move,v. 去,,0,0,zk,10,20,p:went/d:gone/i:going/3:goes,,,\n" +
		"went,/went/,past of go,go的过去式,,0,0,,5000,6000,,,\n" +
		"zebra,/ˈziːbrə/,an animal,n. 斑马,n,1,0,,8000,9000,,,\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tsvPath := filepath.Join(dir, "cmn.txt")
	tsv := "I ate an apple.\t我吃了一个苹果。\tCC-BY\n" +
		"Go home now.\t现在回家。\tCC-BY\n" +
		"He went home.\t他回家了。\tCC-BY\n" +
		"The apple is red.\t苹果是红的。\tCC-BY\n"
	if err := os.WriteFile(tsvPath, []byte(tsv), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "dict.db")
	stats, err := dictbuild.Build(csvPath, tsvPath, out)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 4 || stats.Sentences != 4 {
		t.Fatalf("stats=%+v", stats)
	}

	d, err := dict.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	es, err := d.SearchEnglish(context.Background(), "app", 10)
	if err != nil || len(es) == 0 || es[0].Word != "apple" {
		t.Fatalf("english=%+v err=%v", es, err)
	}
	if es[0].Phonetic != "/ˈæpl/" || es[0].Translation != "n. 苹果" {
		t.Fatalf("entry=%+v", es[0])
	}

	cs, err := d.SearchChinese(context.Background(), "苹果", 10)
	if err != nil || len(cs) == 0 || cs[0].Word != "apple" {
		t.Fatalf("chinese=%+v err=%v", cs, err)
	}

	sent, err := d.Examples(context.Background(), "go", "p:went/d:gone/i:going/3:goes", 2)
	if err != nil || len(sent) != 2 {
		t.Fatalf("sentences=%+v err=%v", sent, err)
	}
	if sent[0].Source != "tatoeba" {
		t.Fatalf("source=%q", sent[0].Source)
	}

	if _, err := d.Lookup(context.Background(), "apple"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Lookup(context.Background(), "nope"); err != dict.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBuildReadsZippedSentences(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "ecdict.csv")
	if err := os.WriteFile(csvPath, []byte("word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange,detail,audio\ngo,/ɡəʊ/,to move,v. 去,,0,0,zk,10,20,,,\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "cmn-eng.zip")
	makeZip(t, zipPath, "cmn.txt", "Go home now.\t现在回家。\tCC-BY\n")
	out := filepath.Join(dir, "dict.db")
	stats, err := dictbuild.Build(csvPath, zipPath, out)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Sentences != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestBuildSkipsBadSentences(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "ecdict.csv")
	if err := os.WriteFile(csvPath, []byte("word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange,detail,audio\ngo,/ɡəʊ/,to move,v. 去,,0,0,zk,10,20,,,\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tsvPath := filepath.Join(dir, "cmn.txt")
	tsv := "ab\t短\tCC-BY\n" +
		"Go home now.\t现在回家。\tCC-BY\n" +
		"GO HOME NOW.\t现在回家。\tCC-BY\n"
	os.WriteFile(tsvPath, []byte(tsv), 0o644)
	stats, err := dictbuild.Build(csvPath, tsvPath, filepath.Join(dir, "dict.db"))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Sentences != 1 {
		t.Fatalf("dedupe/short filter failed: %+v", stats)
	}
}

func TestBuildTurnsEscapedNewlinesIntoLineBreaks(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "ecdict.csv")
	content := "word,phonetic,definition,translation,pos,collins,oxford,tag,bnc,frq,exchange,detail,audio\n" +
		"apple,/ˈæpl/,a round fruit,n. 苹果\\n一个苹果,n,3,1,zk,100,200,,,\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tsvPath := filepath.Join(dir, "cmn.txt")
	if err := os.WriteFile(tsvPath, []byte("I ate an apple.\t我吃了一个苹果。\tCC-BY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "dict.db")
	if _, err := dictbuild.Build(csvPath, tsvPath, out); err != nil {
		t.Fatal(err)
	}
	d, err := dict.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	entry, err := d.Lookup(context.Background(), "apple")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Translation != "n. 苹果\n一个苹果" {
		t.Fatalf("translation=%q", entry.Translation)
	}
}
