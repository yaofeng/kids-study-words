package web

import (
	"fmt"
	"time"

	"github.com/yaofeng/kids-study-words/internal/store"
)

// DayGroup is one local calendar day of recall history.
type DayGroup struct {
	Date    string
	Label   string
	Known   int
	Unknown int
	Logs    []store.RecallLog
}

// GroupLogs buckets logs by local calendar day, preserving input order.
func GroupLogs(logs []store.RecallLog, loc *time.Location) []DayGroup {
	if loc == nil {
		loc = time.Local
	}
	var groups []DayGroup
	index := make(map[string]int)
	for _, log := range logs {
		local := log.ReviewedAt.In(loc)
		date := local.Format("2006-01-02")
		i, ok := index[date]
		if !ok {
			i = len(groups)
			index[date] = i
			groups = append(groups, DayGroup{
				Date:  date,
				Label: fmt.Sprintf("%d 月 %d 日 · %s", local.Month(), local.Day(), weekdayNames[local.Weekday()]),
			})
		}
		g := &groups[i]
		if log.Known {
			g.Known++
		} else {
			g.Unknown++
		}
		g.Logs = append(g.Logs, log)
	}
	return groups
}
