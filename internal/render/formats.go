package render

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"phd/internal/model"
)

// Render は指定フォーマットで論理イベントを出力する。
func Render(w io.Writer, format string, events []model.LogicalEvent, now time.Time, showDetails, showResources bool) error {
	switch format {
	case "", "table":
		Table(w, events, now, showDetails, showResources)
		return nil
	case "json":
		return JSON(w, events)
	case "csv":
		return CSV(w, events)
	case "markdown", "md":
		Markdown(w, events, now, showDetails, showResources)
		return nil
	default:
		return fmt.Errorf("unknown --format %q (table|json|csv|markdown)", format)
	}
}

// JSON は論理イベントを整形 JSON で出力する。
func JSON(w io.Writer, events []model.LogicalEvent) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(events)
}

// CSV はリソース単位の行で出力する（リソースが無いイベントは1行、リソース列は空）。
func CSV(w io.Writer, events []model.LogicalEvent) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	header := []string{
		"EventTypeCode", "Service", "Status", "Category", "Regions", "Start", "End",
		"AccountName", "AccountID", "ResourceRegion", "Resource", "ResourceStatus",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, e := range events {
		base := []string{
			e.EventTypeCode, e.Service, e.StatusCode, e.Category,
			strings.Join(e.Regions, " "), FormatTime(e.StartTime), FormatTime(e.EndTime),
		}
		if len(e.Resources) == 0 {
			if err := cw.Write(append(base, "", "", "", "", "")); err != nil {
				return err
			}
			continue
		}
		for _, r := range e.Resources {
			row := append(append([]string{}, base...), r.AccountName, r.AccountID, r.Region, r.Value, r.Status)
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}
	return cw.Error()
}

// Markdown はテーブルと、変更内容説明・影響リソースのサブセクションを出力する。
func Markdown(w io.Writer, events []model.LogicalEvent, now time.Time, showDetails, showResources bool) {
	fmt.Fprintln(w, "| SERVICE | STATUS | IN | START | EVENT_TYPE | CATEGORY | REGIONS | ACCT | RES |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---:|---:|")
	for _, e := range events {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %d | %d |\n",
			e.Service, e.StatusCode, Countdown(e.StatusCode, e.StartTime, now), FormatTime(e.StartTime),
			e.EventTypeCode, e.Category, JoinRegions(e.Regions), len(e.Accounts), len(e.Resources))
	}
	if !showDetails && !showResources {
		return
	}
	for _, e := range events {
		if (!showDetails || e.Description == "") && len(e.Resources) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n#### %s [%s] %s — start %s (%s)\n\n",
			e.EventTypeCode, e.Service, e.StatusCode, FormatTime(e.StartTime), Countdown(e.StatusCode, e.StartTime, now))
		if showDetails && e.Description != "" {
			fmt.Fprintf(w, "%s\n", e.Description)
		}
		if len(e.Resources) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n| ACCOUNT | REGION | RESOURCE | STATUS |\n")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, r := range e.Resources {
			fmt.Fprintf(w, "| %s | %s | %s | %s |\n", AccountLabel(r), r.Region, r.Value, OrDash(r.Status))
		}
	}
}
