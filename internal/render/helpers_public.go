package render

import (
	"time"

	"phd/internal/model"
)

// 本ファイルは TUI など render パッケージ外から再利用するための薄い public ラッパー。
// 既存の private ヘルパー（table.go / group.go）はシグネチャを変えず、表示の一貫性を保つ。

// Countdown は開始までの残り時間を短く表す（"ongoing"/"started"/"<1d"/"12d"/"3mo"）。
func Countdown(status string, start, now time.Time) string {
	return countdown(status, start, now)
}

// FormatTime は時刻を "2006-01-02 15:04"（UTC）で整形する（ゼロ値は "-"）。
func FormatTime(t time.Time) string {
	return formatTime(t)
}

// FormatDate は日付のみ "2006-01-02"（表示タイムゾーン）で整形する（ゼロ値は "-"）。一覧の簡略表示用。
func FormatDate(t time.Time) string {
	return formatDate(t)
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

// JoinRegions は region 配列をカンマ連結する（空は "-"）。
func JoinRegions(regions []string) string {
	return joinRegions(regions)
}

// AccountLabel は影響リソースのアカウント表示（"名前 (ID)" / "ID" / "-"）を返す。
func AccountLabel(r model.Resource) string {
	return accountLabel(r)
}

// StatusSummary は status 件数を深刻度順に "open:2 upcoming:5" と整形する。
func StatusSummary(counts map[string]int) string {
	return statusSummary(counts)
}

// GroupLabel は EventGroup の表示ラベル（topicMode なら話題ラベル、無ければ eventTypeCode）。
func GroupLabel(g model.EventGroup, topicMode bool) string {
	return groupLabel(g, topicMode)
}

// OrDash は空文字を "-" に置き換える。
func OrDash(s string) string {
	return orDash(s)
}
