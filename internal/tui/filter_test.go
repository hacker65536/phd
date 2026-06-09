package tui

import "testing"

// rec はテスト用にフィールドを FilterValue へエンコードする。
func rec(svc, cat, st, typ, reg string) string {
	return encodeFV(svc, cat, st, typ, reg)
}

func TestRankFilterPrefixSyntax(t *testing.T) {
	targets := []string{
		rec("RDS", "scheduledChange", "open", "AWS_RDS_ENGINE_UPGRADE", "ap-northeast-1"),
		rec("LAMBDA", "scheduledChange", "open", "AWS_LAMBDA_PLANNED_LIFECYCLE_EVENT", "ap-northeast-1,us-east-1"),
		rec("EC2", "issue", "upcoming", "AWS_EC2_RETIREMENT", "us-east-1"),
	}

	cases := []struct {
		name  string
		query string
		want  []int // 期待する Index 集合（順序は入力順）
	}{
		{"free text exact eventtype", "AWS_RDS_ENGINE_UPGRADE", []int{0}},
		{"free text case-insensitive", "lambda", []int{1}},
		{"svc prefix", "svc:RDS", []int{0}},
		{"cat prefix matches multiple", "cat:scheduledChange", []int{0, 1}},
		{"st prefix", "st:upcoming", []int{2}},
		{"reg prefix", "reg:us-east-1", []int{1, 2}},
		{"type prefix substring", "type:UPGRADE", []int{0}},
		{"regex anchored", "re:^AWS_LAMBDA", []int{1}},
		{"AND of two tokens", "svc:EC2 st:upcoming", []int{2}},
		{"AND with no match", "svc:RDS st:upcoming", nil},
		{"empty query returns all", "", []int{0, 1, 2}},
		{"whitespace query returns all", "   ", []int{0, 1, 2}},
		{"unknown key falls back to free text", "foo:bar", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ranks := rankFilter(tc.query, targets)
			got := make([]int, len(ranks))
			for i, r := range ranks {
				got[i] = r.Index
			}
			if !equalInts(got, tc.want) {
				t.Fatalf("query %q: got %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestRankFilterCommaOR(t *testing.T) {
	targets := []string{
		rec("RDS", "scheduledChange", "open", "AWS_RDS_ENGINE_UPGRADE", "ap-northeast-1"),
		rec("LAMBDA", "accountNotification", "upcoming", "AWS_LAMBDA_PLANNED_LIFECYCLE_EVENT", "us-east-1"),
		rec("EC2", "issue", "closed", "AWS_EC2_RETIREMENT", "eu-west-1"),
	}
	cases := []struct {
		name  string
		query string
		want  []int
	}{
		{"svc OR two", "svc:RDS,LAMBDA", []int{0, 1}},
		{"svc OR three", "svc:RDS,LAMBDA,EC2", []int{0, 1, 2}},
		{"cat OR", "cat:scheduledChange,issue", []int{0, 2}},
		{"st OR", "st:open,upcoming", []int{0, 1}},
		{"reg OR", "reg:ap-northeast-1,eu-west-1", []int{0, 2}},
		{"single value unchanged", "svc:LAMBDA", []int{1}},
		{"OR then AND with another axis", "svc:RDS,EC2 st:closed", []int{2}},
		{"case-insensitive OR", "svc:rds,lambda", []int{0, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ranks := rankFilter(tc.query, targets)
			got := make([]int, len(ranks))
			for i, r := range ranks {
				got[i] = r.Index
			}
			if !equalInts(got, tc.want) {
				t.Fatalf("query %q: got %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestRankFilterInvalidRegexFallsBack(t *testing.T) {
	targets := []string{
		rec("EC2", "issue", "open", "AWS_EC2_RETIREMENT", "us-east-1"),
		rec("RDS", "issue", "open", "AWS_RDS_MAINTENANCE", "us-east-1"),
	}
	// "re:[" は不正な正規表現 → type 部分一致にフォールバック（"[" は eventTypeCode に無い → 0 件）。
	if r := rankFilter("re:[", targets); len(r) != 0 {
		t.Fatalf("invalid regex should fall back to (non-matching) substring, got %d matches", len(r))
	}
	// "re:RETIRE" は妥当 → EC2 のみ。
	r := rankFilter("re:RETIRE", targets)
	if len(r) != 1 || r[0].Index != 0 {
		t.Fatalf("re:RETIRE = %v, want [0]", r)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
