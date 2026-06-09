package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"phd/internal/model"
	"phd/internal/render"
)

// statusStyle は status コード別の表示スタイルを返す（一覧の色分け用）。
// open=黄(対応中), upcoming=シアン(予定), closed=薄色。未知はデフォルト。
func statusStyle(code string) lipgloss.Style {
	switch strings.ToLower(code) {
	case "open":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	case "upcoming":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	case "closed":
		return lipgloss.NewStyle().Faint(true)
	default:
		return lipgloss.NewStyle()
	}
}

// occItem は occurrence 一覧の 1 行（list.Item 実装）。
type occItem struct {
	ev  model.LogicalEvent
	now time.Time
}

func (i occItem) Title() string {
	// status は色分け（幅は Render 前にパディングして整列を保つ）。
	status := statusStyle(i.ev.StatusCode).Render(fmt.Sprintf("%-9s", i.ev.StatusCode))
	t := fmt.Sprintf("%-8s  %s  %s", i.ev.Service, status, i.ev.EventTypeCode)
	return t + eolSuffix(i.ev.EventTypeCode)
}

func (i occItem) Description() string {
	// 一覧は日付のみ（正確な時刻=UTC は詳細画面で確認できる）。
	return fmt.Sprintf("in %s · start %s · %s · %s",
		render.Countdown(i.ev.StatusCode, i.ev.StartTime, i.now),
		render.FormatDate(i.ev.StartTime),
		render.JoinRegions(i.ev.Regions),
		render.OrDash(i.ev.Category),
	)
}

func (i occItem) FilterValue() string {
	return encodeFV(i.ev.Service, i.ev.Category, i.ev.StatusCode, eolKeyword(i.ev.EventTypeCode), strings.Join(i.ev.Regions, ","))
}

// groupItem は group（type/topic）一覧の 1 行（list.Item 実装）。
type groupItem struct {
	g     model.EventGroup
	topic bool
	now   time.Time
}

func (i groupItem) Title() string {
	return fmt.Sprintf("%-8s  %s", i.g.Service, render.GroupLabel(i.g, i.topic)) + eolSuffix(i.g.EventTypeCode)
}

func (i groupItem) Description() string {
	return fmt.Sprintf("%s · next %s · occ %d · %s · %s",
		render.StatusSummary(i.g.StatusCounts),
		render.Countdown(i.g.NextStatus, i.g.NextStart, i.now),
		i.g.OccurrenceCount,
		render.JoinRegions(i.g.Regions),
		render.OrDash(i.g.Category),
	)
}

func (i groupItem) FilterValue() string {
	typ := eolKeyword(i.g.EventTypeCode)
	if i.g.Topic != "" {
		typ += " " + i.g.Topic
	}
	return encodeFV(i.g.Service, i.g.Category, statusKeys(i.g.StatusCounts), typ, strings.Join(i.g.Regions, ","))
}

// isLifecycle は planned-lifecycle（EOL/廃止/移行予告）系イベントか（eventTypeCode のみで判定。詳細取得不要）。
// 調査により deprecated_versions メタデータを持つのはこの系統だけと確認済み。
func isLifecycle(eventTypeCode string) bool {
	return strings.HasSuffix(eventTypeCode, "_PLANNED_LIFECYCLE_EVENT")
}

// eolStyle は EOL ラベルの表示スタイル（廃止予告＝オレンジの太字で目立たせる）。
var eolStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208"))

// eolSuffix は lifecycle 系なら一覧に付ける軽量ラベル（色付き EOL）。
func eolSuffix(eventTypeCode string) string {
	if isLifecycle(eventTypeCode) {
		return "  " + eolStyle.Render("EOL")
	}
	return ""
}

// eolKeyword は lifecycle 系の eventTypeCode に絞り込み用キーワード "eol" を足す
// （`/` で `eol` や `type:eol` と打って EOL 系だけを抽出できるようにする）。
func eolKeyword(eventTypeCode string) string {
	if isLifecycle(eventTypeCode) {
		return eventTypeCode + " eol"
	}
	return eventTypeCode
}

// fvSep は FilterValue 内のフィールド区切り（ユーザーが打たない制御文字）。
const fvSep = "\x1f"

// encodeFV はフィルタ照合用に各フィールドを 1 つの FilterValue 文字列へエンコードする。
// 並びは svc, cat, st, type, reg（decodeFV と対応）。
func encodeFV(svc, cat, st, typ, reg string) string {
	return strings.Join([]string{svc, cat, st, typ, reg}, fvSep)
}

// statusKeys は StatusCounts のキー（open/upcoming/closed）をスペース連結する（st: 照合用）。
func statusKeys(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, " ")
}

// occNoun / groupNoun は Title に使う名詞句（件数は updateTitle 側で前置する）。
func occNoun() string {
	return "occurrence(s)"
}

func groupNoun(groupBy string) string {
	return groupBy + " group(s)"
}

// firstARN は occurrence の代表 ARN（最初の非空）を返す。遅延ロードと occState のキーに使う。
func firstARN(ev model.LogicalEvent) string {
	for _, raw := range ev.RawEvents {
		if raw.Arn != "" {
			return raw.Arn
		}
	}
	return ""
}
