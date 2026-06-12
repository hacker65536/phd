package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hacker65536/phd/internal/model"
)

func sample() []model.LogicalEvent {
	return []model.LogicalEvent{{
		EventTypeCode: "AWS_EC2_RETIREMENT",
		Service:       "EC2",
		Category:      "scheduledChange",
		StatusCode:    "upcoming",
		Regions:       []string{"ap-northeast-1", "us-east-1"},
		StartTime:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		Accounts:      []model.Account{{ID: "111111111111", Name: "prod"}},
		Resources: []model.Resource{
			{AccountID: "111111111111", AccountName: "prod", Region: "us-east-1", Value: "i-0abc", Status: "PENDING"},
		},
	}}
}

func TestCSV_FlattensResources(t *testing.T) {
	var buf bytes.Buffer
	if err := CSV(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "i-0abc") || !strings.Contains(out, "prod") {
		t.Errorf("CSV missing resource row:\n%s", out)
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n"); lines != 1 {
		t.Errorf("CSV rows = %d (header+1), want header+1 data row", lines)
	}
}

func TestCSV_NeutralizesFormulaInjection(t *testing.T) {
	events := []model.LogicalEvent{{
		EventTypeCode: "AWS_EC2_RETIREMENT",
		Service:       "EC2",
		Category:      "scheduledChange",
		StatusCode:    "upcoming",
		StartTime:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		Resources: []model.Resource{
			// アカウント名・リソース値に数式起点の文字を含む細工データ。
			{AccountName: "=cmd|'/c calc'!A1", Region: "us-east-1", Value: "@SUM(1+1)", Status: "+1"},
		},
	}}
	var buf bytes.Buffer
	if err := CSV(&buf, events); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, bad := range []string{",=cmd", ",@SUM", ",+1"} {
		if strings.Contains(out, bad) {
			t.Errorf("CSV cell not neutralized (found %q):\n%s", bad, out)
		}
	}
	for _, want := range []string{",'=cmd|'/c calc'!A1,", ",'@SUM(1+1),", ",'+1\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected neutralized cell %q in:\n%s", want, out)
		}
	}
}

func TestCSV_DoesNotAlterSafeCells(t *testing.T) {
	var buf bytes.Buffer
	if err := CSV(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "'i-0abc") || strings.Contains(out, "'prod") {
		t.Errorf("safe cells should not be quoted:\n%s", out)
	}
}

func TestJSON_RoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	var got []model.LogicalEvent
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 || got[0].Resources[0].Value != "i-0abc" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
