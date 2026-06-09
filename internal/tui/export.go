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

// exportFilename は CSV エクスポート時の自動命名（タイムスタンプ付き）を返す。
// 例: phd-resources-20260610-153012.csv
func exportFilename(now time.Time) string {
	return fmt.Sprintf("phd-resources-%s.csv", now.Format("20060102-150405"))
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
	path := uniquePath(filepath.Join(dir, exportFilename(now)))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// occ を複製し、Resources を「現在表示中」の集合に差し替えて render.CSV を再利用する。
	evCopy := occ
	evCopy.Resources = rows
	if err := render.CSV(f, []model.LogicalEvent{evCopy}); err != nil {
		return "", err
	}
	return path, nil
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
