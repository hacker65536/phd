package render

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hacker65536/phd/internal/model"
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

// csvSafe は CSV フォーミュラ・インジェクション対策。表計算ソフト（Excel / Google Sheets /
// LibreOffice）は先頭が =,+,-,@（およびタブ/CR）のセルを数式として評価しうるため、該当セルの
// 先頭にシングルクォートを付けて無害化する。AWS Health 由来でもアカウント名・リソース値・
// 説明はユーザーが任意に決められるため、出力前に必ず通す。
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// writeCSVRow は各セルを ANSI/制御文字除去（SanitizeCell）→ フォーミュラ中和（csvSafe）の
// 順に通してから 1 行書き出す。cat 等で端末に流された場合の制御文字インジェクションも防ぐ。
func writeCSVRow(cw *csv.Writer, row []string) error {
	for i := range row {
		row[i] = csvSafe(SanitizeCell(row[i]))
	}
	return cw.Write(row)
}

// CSV はリソース単位の行で出力する（リソースが無いイベントは1行、リソース列は空）。
func CSV(w io.Writer, events []model.LogicalEvent) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	header := []string{
		"EventTypeCode", "Service", "Status", "Category", "Regions", "Start", "End",
		"AccountName", "AccountID", "ResourceRegion", "Resource", "ResourceStatus",
	}
	if err := cw.Write(header); err != nil { // ヘッダは固定値のため中和不要
		return err
	}
	for _, e := range events {
		base := []string{
			e.EventTypeCode, e.Service, e.StatusCode, e.Category,
			strings.Join(e.Regions, " "), FormatTime(e.StartTime), FormatTime(e.EndTime),
		}
		if len(e.Resources) == 0 {
			if err := writeCSVRow(cw, append(base, "", "", "", "", "")); err != nil {
				return err
			}
			continue
		}
		for _, r := range e.Resources {
			row := append(append([]string{}, base...), r.AccountName, r.AccountID, r.Region, r.Value, r.Status)
			if err := writeCSVRow(cw, row); err != nil {
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
			SanitizeCell(e.Service), SanitizeCell(e.StatusCode), Countdown(e.StatusCode, e.StartTime, now), FormatTime(e.StartTime),
			SanitizeCell(e.EventTypeCode), SanitizeCell(e.Category), SanitizeCell(JoinRegions(e.Regions)), len(e.Accounts), len(e.Resources))
	}
	if !showDetails && !showResources {
		return
	}
	for _, e := range events {
		if (!showDetails || e.Description == "") && len(e.Resources) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n#### %s [%s] %s — start %s (%s)\n\n",
			SanitizeCell(e.EventTypeCode), SanitizeCell(e.Service), SanitizeCell(e.StatusCode), FormatTime(e.StartTime), Countdown(e.StatusCode, e.StartTime, now))
		if showDetails && e.Description != "" {
			fmt.Fprintf(w, "%s\n", SanitizeText(e.Description))
		}
		if len(e.Resources) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n| ACCOUNT | REGION | RESOURCE | STATUS |\n")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, r := range e.Resources {
			fmt.Fprintf(w, "| %s | %s | %s | %s |\n", SanitizeCell(AccountLabel(r)), SanitizeCell(r.Region), SanitizeCell(r.Value), SanitizeCell(OrDash(r.Status)))
		}
	}
}
