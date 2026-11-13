// Command dictbuild builds dict.db from ECDICT and Tatoeba sources.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/yaofeng/kids-study-words/internal/dictbuild"
)

func main() {
	ecdict := flag.String("ecdict", "", "path to ecdict.csv")
	sentences := flag.String("sentences", "", "path to Tatoeba cmn-eng.txt or .zip")
	out := flag.String("out", "data/dict.db", "output dict.db path")
	flag.Parse()

	if *ecdict == "" || *sentences == "" {
		fmt.Fprintln(os.Stderr, "usage: dictbuild -ecdict <ecdict.csv> -sentences <cmn-eng.zip|.txt> -out <dict.db>")
		os.Exit(2)
	}
	stats, err := dictbuild.Build(*ecdict, *sentences, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dictbuild:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d entries, %d sentences\n", *out, stats.Entries, stats.Sentences)
}
