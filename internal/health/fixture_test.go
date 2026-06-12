package health

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hacker65536/phd/internal/model"
)

func sampleFixture() *Fixture {
	return &Fixture{
		CapturedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Events: []model.Event{{
			Arn:           "arn:aws:health:us-east-1::event/RDS/AWS_RDS_MAINTENANCE/abc",
			Service:       "RDS",
			EventTypeCode: "AWS_RDS_MAINTENANCE",
			Region:        "us-east-1",
			StatusCode:    "open",
			StartTime:     time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
			LastUpdated:   time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		}},
		Details: map[string]Detail{
			"arn:aws:health:us-east-1::event/RDS/AWS_RDS_MAINTENANCE/abc": {
				Description: "Account 123456789012 must patch instance i-0abc123def4567890.",
				Metadata:    map[string]string{"deprecated": "RDS 12"},
			},
		},
		Resources: map[string][]model.Resource{
			"arn:aws:health:us-east-1::event/RDS/AWS_RDS_MAINTENANCE/abc": {
				{AccountID: "123456789012", AccountName: "Acme Prod", Region: "us-east-1", Value: "i-0abc123def4567890", Status: "PENDING"},
			},
		},
		Affected: map[string][]string{
			"arn:aws:health:us-east-1::event/RDS/AWS_RDS_MAINTENANCE/abc": {"123456789012"},
		},
		Accounts: map[string]string{"123456789012": "Acme Prod"},
	}
}

// 短絡: fixture を渡した Client は AWS を呼ばず fixture の値を返す（api は nil）。
func TestFixtureClientShortCircuits(t *testing.T) {
	fx := sampleFixture()
	c := NewFixture("demo|us-east-1", fx)
	ctx := context.Background()

	events, err := c.FetchEvents(ctx, true, Query{})
	if err != nil || len(events) != 1 || events[0].Service != "RDS" {
		t.Fatalf("FetchEvents = %v, %v", events, err)
	}
	arn := events[0].Arn

	d, err := c.FetchDetails(ctx, true, arn)
	if err != nil || !strings.Contains(d.Description, "must patch") {
		t.Fatalf("FetchDetails = %+v, %v", d, err)
	}

	batch, err := c.FetchDetailsBatch(ctx, true, []DetailReq{{Arn: arn}})
	if err != nil || batch[arn].Metadata["deprecated"] != "RDS 12" {
		t.Fatalf("FetchDetailsBatch = %+v, %v", batch, err)
	}

	res, err := c.FetchResources(ctx, true, arn, "us-east-1")
	if err != nil || len(res) != 1 || res[0].Value != "i-0abc123def4567890" {
		t.Fatalf("FetchResources = %+v, %v", res, err)
	}

	ids, err := c.fetchAffectedAccountsOrg(ctx, arn)
	if err != nil || len(ids) != 1 || ids[0] != "123456789012" {
		t.Fatalf("fetchAffectedAccountsOrg = %v, %v", ids, err)
	}
}

// Rebase: 時刻が now 基準にシフトする（CapturedAt→now、相対関係は保持）。
func TestRebaseFixture(t *testing.T) {
	fx := sampleFixture()
	orig := fx.Events[0].StartTime
	now := time.Now().UTC()
	RebaseFixture(fx, now)

	shift := now.Sub(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if got, want := fx.Events[0].StartTime, orig.Add(shift); !got.Equal(want) {
		t.Fatalf("StartTime = %v, want %v", got, want)
	}
	if !fx.CapturedAt.Equal(now) {
		t.Fatalf("CapturedAt = %v, want %v", fx.CapturedAt, now)
	}
}

// Rebase: CapturedAt がゼロなら何もしない。
func TestRebaseFixtureZeroCaptured(t *testing.T) {
	fx := sampleFixture()
	fx.CapturedAt = time.Time{}
	orig := fx.Events[0].StartTime
	RebaseFixture(fx, time.Now())
	if !fx.Events[0].StartTime.Equal(orig) {
		t.Fatalf("StartTime changed despite zero CapturedAt")
	}
}

// Scrub: アカウント ID・リソース値・アカウント名・任意トークンが一貫置換され、PII が消える。
func TestScrub(t *testing.T) {
	fx := sampleFixture()
	fx.Scrub(map[string]string{"Acme": "Globex"})

	// 構造フィールド。
	r := fx.Resources["arn:aws:health:us-east-1::event/RDS/AWS_RDS_MAINTENANCE/abc"][0]
	if r.AccountID == "123456789012" {
		t.Errorf("AccountID not scrubbed: %s", r.AccountID)
	}
	if !strings.HasPrefix(r.Value, "i-0demo") {
		t.Errorf("Value not scrubbed (prefix preserved): %s", r.Value)
	}
	if !strings.HasPrefix(r.AccountName, "Demo Account") {
		t.Errorf("AccountName not scrubbed: %s", r.AccountName)
	}

	// 自由文（説明）の中の ID・トークンも消えていること。
	desc := fx.Details["arn:aws:health:us-east-1::event/RDS/AWS_RDS_MAINTENANCE/abc"].Description
	if strings.Contains(desc, "123456789012") || strings.Contains(desc, "i-0abc123def4567890") {
		t.Errorf("description still contains PII: %q", desc)
	}

	// 影響アカウント ID とアカウント名マップも一貫して置換されていること。
	gotID := fx.Affected["arn:aws:health:us-east-1::event/RDS/AWS_RDS_MAINTENANCE/abc"][0]
	if gotID != r.AccountID {
		t.Errorf("Affected id %s != resource AccountID %s (inconsistent mapping)", gotID, r.AccountID)
	}
	if _, ok := fx.Accounts["123456789012"]; ok {
		t.Errorf("Accounts still keyed by real id")
	}
	if _, ok := fx.Accounts[r.AccountID]; !ok {
		t.Errorf("Accounts not re-keyed to scrubbed id %s", r.AccountID)
	}
}

// Scrub: 自由文中のメール・IPv4 が決定論的にマスクされ、バージョン番号は誤検知しない。
func TestScrubMasksEmailAndIP(t *testing.T) {
	const arn = "arn:aws:health:us-east-1::event/RDS/AWS_RDS_MAINTENANCE/abc"
	fx := sampleFixture()
	fx.Details[arn] = Detail{
		Description: "Contact ops@acme.com or sre@acme.com about host 10.20.30.40. Upgrade to 1.2.3.4 client; ping 10.20.30.40 again.",
		Metadata:    map[string]string{"note": "escalate to ops@acme.com"},
	}
	fx.Scrub(nil)

	desc := fx.Details[arn].Description
	note := fx.Details[arn].Metadata["note"]

	if strings.Contains(desc, "acme.com") || strings.Contains(note, "acme.com") {
		t.Errorf("email not masked: desc=%q note=%q", desc, note)
	}
	if strings.Contains(desc, "10.20.30.40") {
		t.Errorf("IP not masked: %q", desc)
	}
	// 同じメールは同じプレースホルダに（説明と metadata で一貫）。
	if !strings.Contains(desc, "demo1@example.com") || !strings.Contains(note, "demo1@example.com") {
		t.Errorf("email mapping not deterministic/consistent: desc=%q note=%q", desc, note)
	}
	// IP はドキュメント用レンジへ。
	if !strings.Contains(desc, "192.0.2.1") {
		t.Errorf("IP not mapped to doc range: %q", desc)
	}
	// "1.2.3.4" はバージョン番号ではなく有効 IP なのでマスクされる（範囲内）。
	if strings.Contains(desc, "1.2.3.4") {
		t.Errorf("valid in-range dotted quad should be masked: %q", desc)
	}
}

// validIPv4: オクテット範囲外（バージョン番号等）は IP とみなさない。
func TestValidIPv4(t *testing.T) {
	cases := map[string]bool{
		"10.20.30.40":     true,
		"255.255.255.255": true,
		"1.2.3.4":         true,
		"256.1.1.1":       false, // オクテット > 255
		"1.2.3":           false, // 3 要素
		"1.2.3.4.5":       false, // 5 要素
	}
	for in, want := range cases {
		if got := validIPv4(in); got != want {
			t.Errorf("validIPv4(%q) = %v, want %v", in, got, want)
		}
	}
}
