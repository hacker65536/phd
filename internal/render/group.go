package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"phd/internal/model"
)

// RenderGroups は eventTypeCode/topic 単位のロールアップを出力する。
// topicMode が true のとき TOPIC 列（話題ラベル）を表示する。
func RenderGroups(w io.Writer, format string, groups []model.EventGroup, now time.Time, topicMode, showDetails, showResources, showOccurrences bool) error {
	switch format {
	case "", "table":
		GroupTable(w, groups, now, topicMode, showDetails, showResources, showOccurrences)
		return nil
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(groups)
	case "markdown", "md":
		GroupMarkdown(w, groups, now, topicMode)
		return nil
	case "csv":
		return groupCSV(w, groups, now, topicMode)
	default:
		return fmt.Errorf("unknown --format %q (table|json|csv|markdown)", format)
	}
}

// GroupTable はファミリーのサマリ表＋（オプションで）説明・リソース・配下日程を出力する。
func GroupTable(w io.Writer, groups []model.EventGroup, now time.Time, topicMode, showDetails, showResources, showOccurrences bool) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	if topicMode {
		fmt.Fprintln(tw, "SERVICE\tSTATUS\tNEXT\tOCC\tREGIONS\tCATEGORY\tACCT\tRES\tTOPIC")
	} else {
		fmt.Fprintln(tw, "SERVICE\tSTATUS\tNEXT\tOCC\tREGIONS\tCATEGORY\tACCT\tRES\tEVENT_TYPE")
	}
	for _, g := range groups {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%d\t%d\t%s\n",
			g.Service,
			statusSummary(g.StatusCounts),
			countdown(g.NextStatus, g.NextStart, now),
			g.OccurrenceCount,
			joinRegions(g.Regions),
			g.Category,
			g.AccountCount,
			g.ResourceCount,
			groupLabel(g, topicMode),
		)
	}
	tw.Flush()

	if !showDetails && !showResources && !showOccurrences {
		return
	}
	for _, g := range groups {
		hasDetail := showDetails && g.Description != ""
		hasRes := showResources && len(g.Resources) > 0
		hasOcc := showOccurrences && len(g.Occurrences) > 0
		if !hasDetail && !hasRes && !hasOcc {
			continue
		}
		fmt.Fprintf(w, "\n▼ %s [%s] %s — %d occurrence(s), next %s\n",
			groupLabel(g, topicMode), g.Service, statusSummary(g.StatusCounts), g.OccurrenceCount,
			countdown(g.NextStatus, g.NextStart, now))
		if topicMode && g.Topic != "" {
			fmt.Fprintf(w, "  eventTypeCode: %s\n", g.EventTypeCode)
		}
		if hasDetail {
			fmt.Fprintln(w, indent(g.Description, "    "))
		}
		if hasOcc {
			ot := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(ot, "  STATUS\tIN\tSTART\tREGIONS\tACCT\tRES")
			for _, o := range g.Occurrences {
				fmt.Fprintf(ot, "  %s\t%s\t%s\t%s\t%d\t%d\n",
					o.StatusCode, countdown(o.StatusCode, o.StartTime, now), formatTime(o.StartTime),
					joinRegions(o.Regions), len(o.Accounts), len(o.Resources))
			}
			ot.Flush()
		}
		if hasRes {
			fmt.Fprintf(w, "  %d affected resource(s) across the family:\n", len(g.Resources))
			rt := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(rt, "  ACCOUNT\tREGION\tRESOURCE\tSTATUS")
			for _, r := range g.Resources {
				fmt.Fprintf(rt, "  %s\t%s\t%s\t%s\n", accountLabel(r), r.Region, r.Value, orDash(r.Status))
			}
			rt.Flush()
		}
	}
}

// groupLabel は表示ラベル（topicMode なら話題ラベル、無ければ eventTypeCode）。
func groupLabel(g model.EventGroup, topicMode bool) string {
	if topicMode && g.Topic != "" {
		return g.Topic
	}
	return g.EventTypeCode
}

// GroupMarkdown はファミリーのサマリ表を Markdown で出力する。
func GroupMarkdown(w io.Writer, groups []model.EventGroup, now time.Time, topicMode bool) {
	head := "EVENT_TYPE"
	if topicMode {
		head = "TOPIC"
	}
	fmt.Fprintf(w, "| SERVICE | STATUS | NEXT | OCC | REGIONS | CATEGORY | ACCT | RES | %s |\n", head)
	fmt.Fprintln(w, "|---|---|---|---:|---|---|---:|---:|---|")
	for _, g := range groups {
		fmt.Fprintf(w, "| %s | %s | %s | %d | %s | %s | %d | %d | %s |\n",
			g.Service, statusSummary(g.StatusCounts), countdown(g.NextStatus, g.NextStart, now),
			g.OccurrenceCount, joinRegions(g.Regions), g.Category, g.AccountCount, g.ResourceCount, groupLabel(g, topicMode))
	}
}

func groupCSV(w io.Writer, groups []model.EventGroup, now time.Time, topicMode bool) error {
	head := "EventTypeCode"
	if topicMode {
		head = "Topic"
	}
	fmt.Fprintf(w, "Service,Status,Next,Occurrences,Regions,Category,Accounts,Resources,%s\n", head)
	for _, g := range groups {
		fmt.Fprintf(w, "%s,%s,%s,%d,%s,%s,%d,%d,%q\n",
			g.Service, statusSummary(g.StatusCounts), countdown(g.NextStatus, g.NextStart, now),
			g.OccurrenceCount, strings.Join(g.Regions, " "), g.Category, g.AccountCount, g.ResourceCount, groupLabel(g, topicMode))
	}
	return nil
}

// statusSummary は status 件数を深刻度順に "open:2 upcoming:5" のように整形する。
func statusSummary(counts map[string]int) string {
	type kv struct {
		s string
		n int
	}
	var items []kv
	for s, n := range counts {
		items = append(items, kv{s, n})
	}
	rank := map[string]int{"open": 0, "upcoming": 1, "closed": 2}
	sort.SliceStable(items, func(i, j int) bool { return rank[items[i].s] < rank[items[j].s] })
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s:%d", it.s, it.n))
	}
	return strings.Join(parts, " ")
}
