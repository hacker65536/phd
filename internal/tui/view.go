package tui

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"

	"phd/internal/enrich"
	"phd/internal/model"
	"phd/internal/render"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	footerStyle = lipgloss.NewStyle().Faint(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	metaKey     = lipgloss.NewStyle().Faint(true)
	// enum 軸チップ（白文字・青背景）。
	chipStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4")).Bold(true)
	// 選択中の行（反転）。
	cursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6"))
	// 下部 status line（category/status）。控えめなシアン。
	statusLineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	// 詳細見出し直下のサブタイトル（eventMetadata の人間可読な説明）。
	subtitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	// RESOLVED 行は薄く表示。
	resolvedStyle = lipgloss.NewStyle().Faint(true)
)

// flashStyle は一時メッセージの表示スタイル。エラー系（"(" 始まり or "export failed"）は赤。
func flashStyle(s string) lipgloss.Style {
	if strings.HasPrefix(s, "export failed") || strings.HasPrefix(s, "(") {
		return errStyle
	}
	return footerStyle
}

// withFlash はフッタ行末尾に m.flash を付与する（無ければそのまま）。
func (m Model) withFlash(footer string) string {
	if m.flash == "" {
		return footer
	}
	return footer + footerStyle.Render("   — ") + flashStyle(m.flash).Render(m.flash)
}

// View はトップフレームの階層に応じて一覧 / 詳細 / 影響リソース一覧を描画する。
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	switch m.top().level {
	case levelDetail:
		return m.detailView()
	case levelResources:
		return m.resourcesView()
	default:
		return m.listView()
	}
}

func (m Model) listView() string {
	if m.picker != nil {
		return m.pickerView()
	}
	var parts []string
	// 上部: 検索行（自由語）＋チップ行（svc/reg 等。cat/st は除く）。件数は Title に集約。
	if m.searchLineShown() {
		parts = append(parts, m.fi.View())
	}
	if m.chipLineShown() {
		parts = append(parts, m.chipLine())
	}
	parts = append(parts, m.list.View())
	// 下部: category/status の status line（あれば）＋フッタ。
	if m.catStatusShown() {
		parts = append(parts, m.catStatusLine())
	}
	parts = append(parts, m.withFlash(footerStyle.Render(m.listFooter())))
	return strings.Join(parts, "\n")
}

// chipLine は svc/reg 等のチップ行（"filters: [svc:RDS]"）。cat/st は下部 status line に出すので除外。
func (m Model) chipLine() string {
	var b strings.Builder
	b.WriteString(metaKey.Render("filters: "))
	for _, f := range m.top().facets {
		if fld, _ := enumField(f); fld == "cat" || fld == "st" {
			continue
		}
		b.WriteString(chipStyle.Render(" "+f+" ") + " ")
	}
	return b.String()
}

// catStatusLine は category/status の現在値をテキストで表す下部 status line。
func (m Model) catStatusLine() string {
	var parts []string
	if v := facetValue(m.top().facets, "cat"); v != "" {
		parts = append(parts, "category: "+strings.ReplaceAll(v, ",", ", "))
	}
	if v := facetValue(m.top().facets, "st"); v != "" {
		parts = append(parts, "status: "+strings.ReplaceAll(v, ",", ", "))
	}
	return statusLineStyle.Render("▸ " + strings.Join(parts, "   "))
}

// facetValue は指定軸のチップ値（":" の後ろ）を返す（無ければ ""）。
func facetValue(facets []string, field string) string {
	for _, f := range facets {
		if fld, _ := enumField(f); fld == field {
			return f[strings.Index(f, ":")+1:]
		}
	}
	return ""
}

func (m Model) listFooter() string {
	if m.filtering {
		return "enter: apply (svc:/cat:/st:/reg: → chip, ほかは自由語)   esc: cancel"
	}
	depth := ""
	if len(m.stack) > 1 {
		depth = "  esc: clear/back"
	}
	return "↑/↓: move   enter: drill down" + depth + "   /: filter   c: category   s: status   g: group   e: export   q: quit"
}

// pickerView は category / status のチェックボックス選択パネルを描画する。
func (m Model) pickerView() string {
	counts := m.pickerCounts(m.picker.field)
	var b strings.Builder
	b.WriteString(headerStyle.Render(m.picker.title) + "\n\n")
	for i, v := range m.picker.values {
		box := "[ ]"
		if m.pickerSel[v] {
			box = "[x]"
		}
		line := fmt.Sprintf(" %s %-20s %3d ", box, v, counts[v])
		if i == m.pickerCursor {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + footerStyle.Render("↑/↓: move   space: toggle   enter: apply   esc: cancel"))
	return b.String()
}

// viewportPage は m.detail viewport を使うページ（詳細 / 影響リソース）の共通レイアウト。
// ヘッダ＋本文＋（flash 付き）フッタを組み立てる。
func (m Model) viewportPage(headerText, footerText string) string {
	header := headerStyle.Render(headerText)
	footer := m.withFlash(footerStyle.Render(footerText))
	return header + "\n" + m.detail.View() + "\n" + footer
}

func (m Model) detailView() string {
	return m.viewportPage("▼ "+m.top().title, m.detailFooter())
}

func (m Model) detailFooter() string {
	st := m.state[m.top().occKey]
	loading := ""
	if st != nil && (st.dState == stateLoading || st.rState == stateLoading) {
		loading = "  " + m.spinner.View() + " loading…"
	}
	return "↑/↓: scroll   enter: resources   r: refresh   esc/⌫: back   q: quit" + loading
}

// resourcesView は影響リソース一覧（3ページ目）を描画する。
func (m Model) resourcesView() string {
	t := m.top()
	st := m.state[t.occKey]
	total, shown := 0, 0
	if st != nil {
		total = len(st.resources)
		shown = len(m.visibleResources(st))
	}
	count := fmt.Sprintf("%d", total)
	if shown != total { // RESOLVED 非表示で減っているとき
		count = fmt.Sprintf("%d/%d", shown, total)
	}
	loading := ""
	if st != nil && st.rState == stateLoading {
		loading = "  " + m.spinner.View() + " loading…"
	}
	toggle := "a: show resolved"
	if m.showResolved {
		toggle = "a: hide resolved"
	}
	headerText := fmt.Sprintf("▼ %s — affected resources (%s)", t.title, count)
	footerText := "↑/↓: scroll   " + toggle + "   e: export csv   r: refresh   esc/⌫: back   q: quit" + loading
	return m.viewportPage(headerText, footerText)
}

// detailContent は 1 occurrence の詳細（メタ情報＋説明＋影響リソース）をテキストで組み立てる。
func (m Model) detailContent(occ model.LogicalEvent, st *occState) string {
	var b strings.Builder

	// eventTypeCode の人間可読な説明（eventMetadata=deprecated_versions 等）を見出し直下に出す。
	// 例: "AWS Lambda end of support for Python 3.9"。遅延ロード完了後に表示。
	if st != nil && st.dState == stateLoaded {
		if topic := enrich.TopicLabel(st.detail.Metadata); topic != "" {
			b.WriteString(subtitleStyle.Render(topic) + "\n\n")
		}
	}

	// メタ情報（取得済みの基本フィールド）。
	fmt.Fprintf(&b, "%s %s\n", metaKey.Render("service: "), occ.Service)
	fmt.Fprintf(&b, "%s %s (%s)\n", metaKey.Render("status:  "), occ.StatusCode,
		render.Countdown(occ.StatusCode, occ.StartTime, m.now))
	fmt.Fprintf(&b, "%s %s\n", metaKey.Render("category:"), render.OrDash(occ.Category))
	fmt.Fprintf(&b, "%s %s\n", metaKey.Render("start:   "), zonedTime(occ.StartTime))
	fmt.Fprintf(&b, "%s %s\n", metaKey.Render("end:     "), zonedTime(occ.EndTime))
	fmt.Fprintf(&b, "%s %s\n", metaKey.Render("regions: "), render.JoinRegions(occ.Regions))
	b.WriteString("\n")

	// 説明（遅延ロード）。
	b.WriteString(headerStyle.Render("Description"))
	b.WriteByte('\n')
	switch {
	case st == nil || st.dState == stateNone:
		b.WriteString("(not loaded)\n")
	case st.dState == stateLoading:
		b.WriteString("Loading…\n")
	case st.dState == stateFailed:
		b.WriteString(errStyle.Render(fmt.Sprintf("failed: %v", st.err)) + "\n")
	case st.detail.Description != "":
		b.WriteString(st.detail.Description)
		b.WriteByte('\n')
	default:
		b.WriteString("(no description)\n")
	}
	b.WriteString("\n")

	// 影響リソースは独立ページ（level 3）。ここでは件数と入口だけ示す。
	b.WriteString(metaKey.Render("Affected resources: ") + m.resourceHint(st) + "\n")

	return b.String()
}

// resourceHint は詳細ページ末尾に出す影響リソースの件数/状態と入口の案内。
func (m Model) resourceHint(st *occState) string {
	switch {
	case st == nil || st.rState == stateNone:
		return "(not loaded)"
	case st.rState == stateLoading:
		return "loading…"
	case st.rState == stateFailed:
		return errStyle.Render("failed")
	case len(st.resources) == 0:
		return "none"
	default:
		return fmt.Sprintf("%d", len(st.resources)) + metaKey.Render("   — Enter で一覧")
	}
}

// resourcesContent は影響リソース一覧ページ（level 3）の本文を組み立てる。
// アカウント順にソートし、既定では RESOLVED を隠す（a で全表示）。
func (m Model) resourcesContent(st *occState) string {
	switch {
	case st == nil || st.rState == stateNone:
		return "(not loaded)\n"
	case st.rState == stateLoading:
		return "Loading…\n"
	case st.rState == stateFailed:
		return errStyle.Render(fmt.Sprintf("failed: %v", st.err)) + "\n"
	case len(st.resources) == 0:
		return "(none)\n"
	}
	var b strings.Builder
	if hidden := countResolved(st.resources); !m.showResolved && hidden > 0 {
		b.WriteString(metaKey.Render(fmt.Sprintf("RESOLVED %d 件を非表示中（a で全表示）", hidden)) + "\n\n")
	}
	rows := m.visibleResources(st)
	if len(rows) == 0 {
		b.WriteString(metaKey.Render("表示できるリソースがありません（a で RESOLVED を表示）") + "\n")
		return b.String()
	}
	b.WriteString(resourceTable(rows))
	return b.String()
}

// visibleResources はソート済み・（showResolved に応じて）フィルタ済みのリソースを返す。
func (m Model) visibleResources(st *occState) []model.Resource {
	if st == nil {
		return nil
	}
	rows := sortResourcesByAccount(st.resources)
	if m.showResolved {
		return rows
	}
	out := make([]model.Resource, 0, len(rows))
	for _, r := range rows {
		if !isResolved(r.Status) {
			out = append(out, r)
		}
	}
	return out
}

// sortResourcesByAccount はアカウント（名前→ID）→ region → リソース値 の順でソートした複製を返す。
func sortResourcesByAccount(res []model.Resource) []model.Resource {
	out := append([]model.Resource(nil), res...)
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := render.AccountLabel(out[i]), render.AccountLabel(out[j]); a != b {
			return a < b
		}
		if out[i].Region != out[j].Region {
			return out[i].Region < out[j].Region
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// resourceTable はリソース表を組み立てる。RESOLVED 行は薄色にする。
// tabwriter は ANSI のバイト幅を桁に数えてしまうため、整列後の行に対して色付けする。
func resourceTable(rows []model.Resource) string {
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ACCOUNT\tREGION\tRESOURCE\tSTATUS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			render.AccountLabel(r), render.OrDash(r.Region), r.Value, render.OrDash(r.Status))
	}
	tw.Flush()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, r := range rows { // lines[0] はヘッダ、lines[i+1] が rows[i]
		if i+1 < len(lines) && isResolved(r.Status) {
			lines[i+1] = resolvedStyle.Render(lines[i+1])
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func isResolved(status string) bool {
	return strings.EqualFold(status, "RESOLVED")
}

func countResolved(res []model.Resource) int {
	n := 0
	for _, r := range res {
		if isResolved(r.Status) {
			n++
		}
	}
	return n
}

// zonedTime は詳細画面用に時刻を "YYYY-MM-DD HH:MM <ZONE>" で表示する（ゼロ値は "-"）。
// ゾーンは --tz で切替（既定 UTC）。
func zonedTime(t time.Time) string {
	s := render.FormatTime(t)
	if s == "-" {
		return s
	}
	return s + " " + render.ZoneAbbrev(t)
}
