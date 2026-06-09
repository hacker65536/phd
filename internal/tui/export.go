package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"phd/internal/model"
	"phd/internal/render"
)

// exportFilename は CSV エクスポート時の自動命名（prefix＋タイムスタンプ）を返す。
// 例: exportFilename("phd-resources", t) → phd-resources-20260610-153012.csv
func exportFilename(prefix string, now time.Time) string {
	return fmt.Sprintf("%s-%s.csv", prefix, now.Format("20060102-150405"))
}

// writeLogicalCSV は論理イベント列を CSV ファイルへ書き出し、実際に書いたパスを返す。
// prefix で命名を切替（リソース export / イベント一覧 export 共通）。
func writeLogicalCSV(dir, prefix string, events []model.LogicalEvent, now time.Time) (string, error) {
	path := uniquePath(filepath.Join(dir, exportFilename(prefix, now)))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := render.CSV(f, events); err != nil {
		return "", err
	}
	return path, nil
}

// uniquePath は path が既存なら "-2", "-3" … を拡張子の前に付けて未使用パスを返す。
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d%s", base, i, ext)
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
}

// writeResourcesCSV は 1 occurrence の影響リソース（rows）を CSV ファイルに書き出し、
// 実際に書いたパスを返す。列構成は CLI の render.CSV と同一（イベント列＋リソース列）。
// dir/now/rows を引数に取ることで時刻・出力先を注入でき、テスト可能にしている。
func writeResourcesCSV(dir string, occ model.LogicalEvent, rows []model.Resource, now time.Time) (string, error) {
	// occ を複製し、Resources を「現在表示中」の集合に差し替えて render.CSV を再利用する。
	evCopy := occ
	evCopy.Resources = rows
	return writeLogicalCSV(dir, "phd-resources", []model.LogicalEvent{evCopy}, now)
}

// exportEventsCSV は一覧の論理イベント（現在表示中）を CSV ファイルへ書き出す。
// リソースは未ロードのためリソース列は空（＝イベント一覧の CSV）。
func exportEventsCSV(dir string, events []model.LogicalEvent, now time.Time) (string, error) {
	return writeLogicalCSV(dir, "phd-events", events, now)
}

// exportResources は影響リソース一覧（level 3）で押された e を処理し、
// 現在表示中のリソースを CSV に書き出して結果を m.flash に残す。
func (m Model) exportResources() (tea.Model, tea.Cmd) {
	st := m.state[m.top().occKey]
	rows := m.visibleResources(st)
	if st == nil || st.rState != stateLoaded || len(rows) == 0 {
		m.flash = "(no resources to export)"
		return m, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		m.flash = "export failed: " + err.Error()
		return m, nil
	}
	path, err := writeResourcesCSV(dir, m.top().occ, rows, time.Now())
	if err != nil {
		m.flash = "export failed: " + err.Error()
		return m, nil
	}
	m.flash = fmt.Sprintf("exported %d rows → %s", len(rows), filepath.Base(path))
	return m, nil
}

// visibleEvents は現在の一覧（フィルタ反映済み）に出ている論理イベントを集める。
// occurrence 一覧はその行のイベント、group 一覧は各 group 配下の occurrence を平坦化する。
func (m Model) visibleEvents() []model.LogicalEvent {
	var out []model.LogicalEvent
	for _, it := range m.list.Items() {
		switch v := it.(type) {
		case occItem:
			out = append(out, v.ev)
		case groupItem:
			out = append(out, v.g.Occurrences...)
		}
	}
	return out
}

// exportVisibleEvents は一覧（level 0/1）で押された e を処理し、
// 現在表示中のイベントを CSV に書き出して結果を m.flash に残す。
func (m Model) exportVisibleEvents() (tea.Model, tea.Cmd) {
	events := m.visibleEvents()
	if len(events) == 0 {
		m.flash = "(no events to export)"
		return m, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		m.flash = "export failed: " + err.Error()
		return m, nil
	}
	path, err := exportEventsCSV(dir, events, time.Now())
	if err != nil {
		m.flash = "export failed: " + err.Error()
		return m, nil
	}
	m.flash = fmt.Sprintf("exported %d events → %s", len(events), filepath.Base(path))
	return m, nil
}
