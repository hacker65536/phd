// Package render は論理イベントを各種フォーマットで出力する。
package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"phd/internal/model"
)

// Table はマージ済みイベントを tabwriter のテーブルで出力する。
// IN 列は開始までの残り時間（now 基準）。showDetails で変更内容説明、
// showResources で影響リソースを各イベントの下に展開する。
func Table(w io.Writer, events []model.LogicalEvent, now time.Time, showDetails, showResources bool) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVICE\tSTATUS\tIN\tSTART\tEVENT_TYPE\tCATEGORY\tREGIONS\tACCT\tRES")
	for _, e := range events {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
			e.Service,
			e.StatusCode,
			Countdown(e.StatusCode, e.StartTime, now),
			FormatTime(e.StartTime),
			e.EventTypeCode,
			e.Category,
			JoinRegions(e.Regions),
			len(e.Accounts),
			len(e.Resources),
		)
	}
	tw.Flush()

	if !showDetails && !showResources {
		return
	}
	for _, e := range events {
		if (!showDetails || e.Description == "") && len(e.Resources) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n▼ %s [%s] %s  start=%s (%s)\n",
			e.EventTypeCode, e.Service, e.StatusCode, FormatTime(e.StartTime), Countdown(e.StatusCode, e.StartTime, now))
		if showDetails && e.Description != "" {
			fmt.Fprintln(w, indent(e.Description, "    "))
		}
		if len(e.Resources) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %d affected resource(s):\n", len(e.Resources))
		rt := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(rt, "  ACCOUNT\tREGION\tRESOURCE\tSTATUS")
		for _, r := range e.Resources {
			fmt.Fprintf(rt, "  %s\t%s\t%s\t%s\n", AccountLabel(r), r.Region, r.Value, OrDash(r.Status))
		}
		rt.Flush()
	}
}

// displayLoc は時刻表示に使うタイムゾーン（既定 UTC、--tz で変更）。
var displayLoc = time.UTC

// SetDisplayLocation は時刻表示のタイムゾーンを設定する（CLI 起動時に一度だけ呼ぶ想定）。
func SetDisplayLocation(loc *time.Location) {
	if loc != nil {
		displayLoc = loc
	}
}

// ZoneAbbrev は t を表示タイムゾーンに変換したときのゾーン略称（"UTC"/"JST" 等）を返す。
func ZoneAbbrev(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(displayLoc).Format("MST")
}

// ZoneName は現在の表示タイムゾーン名（"UTC"/"Local"/"Asia/Tokyo" 等）。
func ZoneName() string {
	return displayLoc.String()
}

// FormatDate は日付のみ "2006-01-02"（displayLoc）で整形する（ゼロ値は "-"）。
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(displayLoc).Format("2006-01-02")
}

// Countdown は開始までの残り時間を短く表す。進行中/開始済みは "ongoing"/"started"。
func Countdown(status string, start, now time.Time) string {
	if status == "open" {
		return "ongoing"
	}
	if start.IsZero() {
		return "-"
	}
	d := start.Sub(now)
	if d < 0 {
		return "started"
	}
	days := int(d.Hours() / 24)
	switch {
	case d < 24*time.Hour:
		return "<1d"
	case days < 60:
		return fmt.Sprintf("%dd", days)
	default:
		return fmt.Sprintf("%dmo", days/30)
	}
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func JoinRegions(regions []string) string {
	if len(regions) == 0 {
		return "-"
	}
	return strings.Join(regions, ",")
}

func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(displayLoc).Format("2006-01-02 15:04")
}

func OrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func AccountLabel(r model.Resource) string {
	switch {
	case r.AccountName != "" && r.AccountID != "":
		return fmt.Sprintf("%s (%s)", r.AccountName, r.AccountID)
	case r.AccountID != "":
		return r.AccountID
	default:
		return "-"
	}
}
