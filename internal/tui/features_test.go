package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestStatusStyleColors(t *testing.T) {
	if c, ok := statusStyle("open").GetForeground().(lipgloss.Color); !ok || c != "11" {
		t.Fatalf("open foreground = %v, want Color(11)", statusStyle("open").GetForeground())
	}
	if c, ok := statusStyle("upcoming").GetForeground().(lipgloss.Color); !ok || c != "6" {
		t.Fatalf("upcoming foreground = %v, want Color(6)", statusStyle("upcoming").GetForeground())
	}
	if !statusStyle("closed").GetFaint() {
		t.Fatal("closed should be faint")
	}
	// 一覧 Title が壊れていない（サービス・status・eventTypeCode を含む）。
	title := occItem{ev: sampleOccs()[0]}.Title()
	for _, want := range []string{"EC2", "upcoming", "AWS_EC2_RETIREMENT"} {
		if !strings.Contains(title, want) {
			t.Fatalf("Title %q should contain %q", title, want)
		}
	}
}

func TestGroupByCycle(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	if m.top().level != levelOccs {
		t.Fatalf("initial level = %d, want levelOccs", m.top().level)
	}
	// none → type
	m, _ = update(t, m, runes("g"))
	if m.groupBy != "type" || m.top().level != levelGroups {
		t.Fatalf("after 1st g: groupBy=%q level=%d, want type/levelGroups", m.groupBy, m.top().level)
	}
	// type → topic
	m, _ = update(t, m, runes("g"))
	if m.groupBy != "topic" || m.top().level != levelGroups {
		t.Fatalf("after 2nd g: groupBy=%q level=%d, want topic/levelGroups", m.groupBy, m.top().level)
	}
	// topic → none
	m, _ = update(t, m, runes("g"))
	if m.groupBy != "" || m.top().level != levelOccs {
		t.Fatalf("after 3rd g: groupBy=%q level=%d, want none/levelOccs", m.groupBy, m.top().level)
	}
}

func TestGroupByIgnoredWhenDrilled(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m, _ = update(t, m, key(tea.KeyEnter)) // → detail（drill down）
	// detail では list switch に到達せず g は効かない（スクロール扱い）。groupBy 不変。
	m, _ = update(t, m, runes("g"))
	if m.groupBy != "" {
		t.Fatalf("g should not change groupBy when drilled down, got %q", m.groupBy)
	}
}

func TestRefreshCurrentResetsState(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m, _ = update(t, m, key(tea.KeyEnter)) // → detail（loading 発火）
	key := "arn:ec2:1"
	// ロード完了状態にする。
	m, _ = update(t, m, detailLoadedMsg{key: key})
	m, _ = update(t, m, resLoadedMsg{key: key})
	if st := m.state[key]; st == nil || st.dState != stateLoaded || st.rState != stateLoaded {
		t.Fatalf("precondition: states should be loaded")
	}
	// r で再取得 → state は loading に戻り、flash と Cmd が出る。
	m2, cmd := update(t, m, runes("r"))
	if cmd == nil {
		t.Fatal("refresh should return a load Cmd")
	}
	st := m2.state[key]
	if st == nil || st.dState != stateLoading || st.rState != stateLoading {
		t.Fatalf("after r: states = %v/%v, want loading", st.dState, st.rState)
	}
	if !strings.HasPrefix(m2.flash, "refresh") {
		t.Fatalf("flash = %q, want refresh…", m2.flash)
	}
}

func TestExportVisibleEvents(t *testing.T) {
	t.Chdir(t.TempDir())
	m := newTestModel(sampleOccs(), "") // 2 件の occurrence
	m, _ = update(t, m, runes("e"))
	if !strings.HasPrefix(m.flash, "exported") {
		t.Fatalf("flash = %q, want prefix 'exported'", m.flash)
	}
	matches, _ := filepath.Glob("phd-events-*.csv")
	if len(matches) != 1 {
		t.Fatalf("expected 1 events export, got %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	content := string(data)
	for _, want := range []string{"AWS_EC2_RETIREMENT", "AWS_RDS_MAINTENANCE", "EventTypeCode,Service,"} {
		if !strings.Contains(content, want) {
			t.Fatalf("events export missing %q:\n%s", want, content)
		}
	}
}
