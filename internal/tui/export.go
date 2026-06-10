package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hacker65536/phd/internal/model"
	"github.com/hacker65536/phd/internal/render"
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

// doExport は「空チェック → cwd 取得 → 書き出し → flash」の共通フロー。
// write は出力先 dir を受け取り、実際に書いたパスを返す。
func (m Model) doExport(empty bool, emptyMsg string, n int, noun string, write func(dir string) (string, error)) (tea.Model, tea.Cmd) {
	if empty {
		m.flash = emptyMsg
		return m, nil
	}
	dir, err := os.Getwd()
	if err == nil {
		var path string
		if path, err = write(dir); err == nil {
			m.flash = fmt.Sprintf("exported %d %s → %s", n, noun, filepath.Base(path))
			return m, nil
		}
	}
	m.flash = "export failed: " + err.Error()
	return m, nil
}

// exportResources は影響リソース一覧（level 3）で押された e を処理し、
// 現在表示中のリソースを CSV に書き出して結果を m.flash に残す。
func (m Model) exportResources() (tea.Model, tea.Cmd) {
	rows := m.visibleResources(m.state[m.top().occKey])
	occ := m.top().occ
	return m.doExport(len(rows) == 0, "(no resources to export)", len(rows), "rows",
		func(dir string) (string, error) { return writeResourcesCSV(dir, occ, rows, time.Now()) })
}

// visibleEvents は現在の一覧（フィルタ反映済み）に出ている論理イベントを集める。
// occurrence 一覧はその行のイベント、group 一覧は各 group 配下の occurrence を平坦化する。
func (m Model) visibleEvents() []model.LogicalEvent {
	var out []model.LogicalEvent
	for _, it := range m.list.Items() {
		if ei, ok := it.(eventItem); ok {
			out = append(out, ei.events()...)
		}
	}
	return out
}

// exportVisibleEvents は一覧（level 0/1）で押された e を処理し、
// 現在表示中のイベントを CSV に書き出して結果を m.flash に残す。
func (m Model) exportVisibleEvents() (tea.Model, tea.Cmd) {
	events := m.visibleEvents()
	return m.doExport(len(events) == 0, "(no events to export)", len(events), "events",
		func(dir string) (string, error) { return exportEventsCSV(dir, events, time.Now()) })
}
