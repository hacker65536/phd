package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hacker65536/phd/internal/model"
)

func TestExportFilename(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 30, 12, 0, time.UTC)
	if got, want := exportFilename("phd-resources", now), "phd-resources-20260610-153012.csv"; got != want {
		t.Fatalf("exportFilename = %q, want %q", got, want)
	}
	if got, want := exportFilename("phd-events", now), "phd-events-20260610-153012.csv"; got != want {
		t.Fatalf("exportFilename = %q, want %q", got, want)
	}
}

func TestWriteResourcesCSV(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 10, 15, 30, 12, 0, time.UTC)
	occ := model.LogicalEvent{
		EventTypeCode: "AWS_EC2_RETIREMENT",
		Service:       "EC2",
		Category:      "scheduledChange",
		StatusCode:    "upcoming",
		Regions:       []string{"ap-northeast-1"},
	}
	rows := []model.Resource{
		{AccountID: "111", AccountName: "alice", Region: "ap-northeast-1", Value: "i-aaa", Status: "PENDING"},
		{AccountID: "222", AccountName: "bob", Region: "us-east-1", Value: "i-bbb", Status: "IMPAIRED"},
	}

	path, err := writeResourcesCSV(dir, occ, rows, now)
	if err != nil {
		t.Fatalf("writeResourcesCSV: %v", err)
	}

	// (a) 期待パスにファイルが出来る。
	if want := filepath.Join(dir, "phd-resources-20260610-153012.csv"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %q: %v", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	content := string(data)
	// (b) リソース値とイベント列が含まれる。
	for _, want := range []string{"i-aaa", "i-bbb", "AWS_EC2_RETIREMENT", "alice", "bob"} {
		if !strings.Contains(content, want) {
			t.Fatalf("export missing %q:\n%s", want, content)
		}
	}
	// (c) ヘッダ行がある。
	if !strings.HasPrefix(content, "EventTypeCode,Service,") {
		t.Fatalf("export should start with header row:\n%s", content)
	}
}

func TestUniquePathAvoidsCollision(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "phd-resources-x.csv")
	if got := uniquePath(p); got != p {
		t.Fatalf("uniquePath on free path = %q, want %q", got, p)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "phd-resources-x-2.csv"); uniquePath(p) != want {
		t.Fatalf("uniquePath collision = %q, want %q", uniquePath(p), want)
	}
}

func TestExportKeyFlow(t *testing.T) {
	t.Chdir(t.TempDir()) // エクスポートはカレントに書くため隔離（Go 1.20+ の t.Chdir）

	res := []model.Resource{
		{AccountID: "111", AccountName: "alice", Region: "ap-northeast-1", Value: "i-aaa", Status: "PENDING"},
		{AccountID: "222", AccountName: "bob", Region: "us-east-1", Value: "i-bbb", Status: "RESOLVED"},
	}

	m := newTestModel(sampleOccs(), "")
	m, _ = update(t, m, key(tea.KeyEnter))                              // → detail
	m, _ = update(t, m, resLoadedMsg{key: "arn:ec2:1", resources: res}) // リソース注入
	m, _ = update(t, m, key(tea.KeyEnter))                              // → resources page
	if m.top().level != levelResources {
		t.Fatalf("expected resources level, got %d", m.top().level)
	}

	// 既定（RESOLVED 非表示）→ visible は 1 件。
	visN := len(m.visibleResources(m.state["arn:ec2:1"]))
	if visN != 1 {
		t.Fatalf("default visible = %d, want 1", visN)
	}

	m, _ = update(t, m, runes("e"))
	if !strings.HasPrefix(m.flash, "exported") {
		t.Fatalf("flash = %q, want prefix 'exported'", m.flash)
	}

	// カレントに CSV が出来ている。
	matches, _ := filepath.Glob("phd-resources-*.csv")
	if len(matches) != 1 {
		t.Fatalf("expected 1 export file, got %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	content := string(data)
	// visible（PENDING の i-aaa）は含まれ、非表示の RESOLVED（i-bbb）は含まれない。
	if !strings.Contains(content, "i-aaa") {
		t.Fatalf("export should contain visible resource i-aaa:\n%s", content)
	}
	if strings.Contains(content, "i-bbb") {
		t.Fatalf("export should not contain hidden RESOLVED resource i-bbb:\n%s", content)
	}
}
