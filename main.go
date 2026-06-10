// phd は AWS Health Dashboard をローカル CLI で確認するツール。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
	_ "time/tzdata" // IANA タイムゾーンDBを埋め込み（--tz Asia/Tokyo 等を OS 非依存で解決）

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hacker65536/phd/internal/awsx"
	"github.com/hacker65536/phd/internal/cache"
	"github.com/hacker65536/phd/internal/enrich"
	"github.com/hacker65536/phd/internal/group"
	"github.com/hacker65536/phd/internal/health"
	"github.com/hacker65536/phd/internal/merge"
	"github.com/hacker65536/phd/internal/model"
	"github.com/hacker65536/phd/internal/orgs"
	"github.com/hacker65536/phd/internal/query"
	"github.com/hacker65536/phd/internal/render"
	"github.com/hacker65536/phd/internal/tui"
)

// ビルド時のバージョン情報。goreleaser / `go build -ldflags "-X main.version=..."` で上書きされる。
// 素の `go build` / `go run` では下記の既定値（開発ビルド）になる。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// resolveVersion は ldflags 未注入（version=="dev"）のとき、`go install pkg@vX.Y.Z` が
// バイナリに埋め込むモジュールバージョン（runtime/debug）にフォールバックする。
// これにより goreleaser 経由でなくても `phd version` が意味あるバージョンを返す。
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "phd",
		Short:         "AWS Health Dashboard をローカルで確認する CLI",
		Version:       resolveVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("phd {{.Version}}\n")
	root.AddCommand(eventsCmd(), versionCmd())
	return root
}

// versionCmd は `phd version` でバージョン・コミット・ビルド日時・Go バージョンを表示する。
// `phd --version` は cobra 組み込みで短い形（phd <version>）を出す。
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "バージョン情報を表示",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("phd %s\n  commit: %s\n  built:  %s\n  go:     %s\n",
				resolveVersion(), commit, date, runtime.Version())
		},
	}
}

type eventsOpts struct {
	profile     string
	scope       string
	status      string
	within      string
	since       string
	starting    string
	openSince   string
	service     string
	region      string
	category    string
	eventType   string
	noMerge     bool
	groupBy     string
	showOcc     bool
	showDetails bool
	showRes     bool
	tui         bool
	tz          string
	format      string
	output      string
	noCache     bool
	refresh     bool
	cacheTTL    string
}

func eventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Health イベント一覧を表示（既定: 進行中＋予定を開始の近い順に）",
		Long: "AWS Health イベントを表示する。\n" +
			"既定は status=open,upcoming（対応が必要なもの）を、開始時刻(startTime)の近い順に並べる。\n" +
			"region をまたぐ同一 eventTypeCode は 1 つの論理イベントに束ねる。\n" +
			"全フラグは config.yaml / 環境変数 PHD_* でも指定可（優先順位: フラグ > 環境変数 > config > 既定）。",
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := optsFromViper(cmd)
			if err != nil {
				return err
			}
			return runEvents(cmd.Context(), o)
		},
	}
	f := cmd.Flags()
	f.String("config", "", "設定ファイル(YAML)。既定: ./config.yaml, ~/.config/phd/config.yaml")
	f.StringP("profile", "p", "", "AWS profile（既定: AWS_PROFILE / default）")
	f.String("scope", "organization", "スコープ: organization|account")
	// 選択（既定はステータス主軸・時間窓なし）
	f.String("status", "", "ステータスを明示指定（カンマ区切り: open,upcoming,closed）。既定 open,upcoming")
	f.String("within", "", "前方ホライズン: 開始が今後この期間内のものだけ（例: 90d/2w）。進行中は常に表示")
	f.String("since", "", "履歴モード: この期間内に更新されたものを closed 含めて表示（例: 30d / YYYY-MM-DD）")
	f.String("starting", "", "開始時刻レンジ（監査用）: A..B（例: 2026-06-01..2026-07-01）")
	f.String("open-since", "90d", "open は直近この期間に更新されたものだけ表示（0 で無効。--since/--starting 時は無視）")
	// 絞り込み（クライアント側）
	f.String("service", "", "サービスで絞り込み（例: RDS, EC2）")
	f.String("filter-region", "", "リージョンで絞り込み（例: ap-northeast-1）")
	f.String("category", "", "カテゴリで絞り込み（issue|scheduledChange|accountNotification）")
	f.String("event-type", "", "eventTypeCode への正規表現")
	// 表示
	f.String("group-by", "", "ロールアップ: type(eventTypeCode単位) | topic(話題=eventMetadata単位)。空=occurrence のまま")
	f.Bool("show-occurrences", false, "--group-by 時: 各ファミリー配下の occurrence(日程)を展開")
	f.Bool("no-merge", false, "region マージせず生イベントを表示")
	f.Bool("show-details", false, "変更内容の説明(latestDescription)を展開")
	f.Bool("show-resources", false, "影響リソースを全アカウント・全リージョン分、平坦テーブルで展開")
	f.Bool("tui", false, "対話的 TUI を起動（一覧→Enter で詳細/影響リソースにドリルダウン）。--format/--output は無視")
	f.String("tz", "", "時刻表示のタイムゾーン（既定 UTC）。例: local, Asia/Tokyo, UTC")
	f.StringP("format", "f", "table", "出力形式: table|json|csv|markdown")
	f.StringP("output", "o", "", "出力先ファイル（既定: 標準出力）")
	f.Bool("no-cache", false, "キャッシュを使わない")
	f.Bool("refresh", false, "キャッシュを無視して再取得（結果は保存）")
	f.String("cache-ttl", "1h", "キャッシュ有効期間（例: 30m, 1h, 6h, 1d）")
	return cmd
}

// optsFromViper はフラグ・config.yaml・環境変数を統合して eventsOpts を構築する。
// 優先順位（Viper 既定）は フラグ（明示指定）> 環境変数(PHD_*) > config > 既定。
func optsFromViper(cmd *cobra.Command) (*eventsOpts, error) {
	v := viper.New()
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return nil, err
	}
	v.SetEnvPrefix("PHD")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	if cfg := v.GetString("config"); cfg != "" {
		v.SetConfigFile(cfg)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config %q: %w", cfg, err)
		}
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		if home, err := os.UserHomeDir(); err == nil {
			v.AddConfigPath(filepath.Join(home, ".config", "phd"))
		}
		if err := v.ReadInConfig(); err != nil {
			if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound {
				return nil, fmt.Errorf("read config: %w", err)
			}
		}
	}
	if used := v.ConfigFileUsed(); used != "" {
		fmt.Fprintf(os.Stderr, "Config: %s\n", used)
	}

	// getStr はスカラー文字列を返す。YAML でリスト指定（- a / - b）された場合は
	// カンマ区切り文字列に変換する（status/service/category/region は複数指定可）。
	getStr := func(key string) string {
		if s := v.GetString(key); s != "" {
			return s
		}
		if xs := v.GetStringSlice(key); len(xs) > 0 {
			return strings.Join(xs, ",")
		}
		return ""
	}

	return &eventsOpts{
		profile:     v.GetString("profile"),
		scope:       v.GetString("scope"),
		status:      getStr("status"),
		within:      v.GetString("within"),
		since:       v.GetString("since"),
		starting:    v.GetString("starting"),
		openSince:   v.GetString("open-since"),
		service:     getStr("service"),
		region:      getStr("filter-region"),
		category:    getStr("category"),
		eventType:   v.GetString("event-type"),
		groupBy:     v.GetString("group-by"),
		showOcc:     v.GetBool("show-occurrences"),
		noMerge:     v.GetBool("no-merge"),
		showDetails: v.GetBool("show-details"),
		showRes:     v.GetBool("show-resources"),
		tui:         v.GetBool("tui"),
		tz:          v.GetString("tz"),
		format:      v.GetString("format"),
		output:      v.GetString("output"),
		noCache:     v.GetBool("no-cache"),
		refresh:     v.GetBool("refresh"),
		cacheTTL:    v.GetString("cache-ttl"),
	}, nil
}

var validStatus = map[string]bool{"open": true, "upcoming": true, "closed": true}

// resolveStatuses は --status の明示指定、なければ既定 {open,upcoming}（--since 時は closed も）を返す。
func resolveStatuses(statusFlag, since string) ([]string, error) {
	if statusFlag != "" {
		var out []string
		for _, s := range strings.Split(statusFlag, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !validStatus[s] {
				return nil, fmt.Errorf("invalid --status %q (open|upcoming|closed)", s)
			}
			out = append(out, s)
		}
		return out, nil
	}
	if since != "" {
		return []string{"open", "upcoming", "closed"}, nil
	}
	return []string{"open", "upcoming"}, nil
}

// parseStartingRange は "A..B" を startTime レンジに変換する（片側省略可）。
func parseStartingRange(s string, now time.Time) (time.Time, time.Time, error) {
	parts := strings.SplitN(s, "..", 2)
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --starting %q (use A..B)", s)
	}
	var from, to time.Time
	var err error
	if p := strings.TrimSpace(parts[0]); p != "" {
		if from, err = query.ParseSince(p, now); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if p := strings.TrimSpace(parts[1]); p != "" {
		if to, err = query.ParseSince(p, now); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	return from, to, nil
}

// fetchResult は loadLogical の出力。enrich 前の論理イベントと、
// 後続の遅延取得（詳細/リソース/アカウント名）に必要なクライアント・キャッシュを束ねる。
// CLI（runEvents の後半）と TUI（tui.Run）の双方が同じ取得経路を共有するための受け渡し型。
type fetchResult struct {
	client  *health.Client
	org     bool
	logical []model.LogicalEvent // フィルタ・マージ・ソート済み（enrich 前）
	now     time.Time
	cache   *cache.Cache
	ns      string
	cfg     aws.Config // org アカウント名解決（orgs.NameMap）用
}

// loadLogical は取得パイプラインの前半（fetch→filter→prune→horizon→merge→sort）を実行する。
// enrich（詳細/影響リソース取得）は行わない。quiet=true のとき stderr への進捗ログを抑制する（TUI 起動時）。
func loadLogical(ctx context.Context, o *eventsOpts, quiet bool) (*fetchResult, error) {
	if o.scope != "organization" && o.scope != "account" {
		return nil, fmt.Errorf("invalid --scope %q (organization|account)", o.scope)
	}
	org := o.scope == "organization"
	now := time.Now().UTC()

	if o.groupBy != "" && o.groupBy != "type" && o.groupBy != "topic" {
		return nil, fmt.Errorf("invalid --group-by %q (type|topic)", o.groupBy)
	}

	// 取得条件（Query）の構築。既定はステータス主軸・時間窓なし。
	statuses, err := resolveStatuses(o.status, o.since)
	if err != nil {
		return nil, err
	}
	q := health.Query{Statuses: statuses}
	if o.since != "" {
		from, perr := query.ParseSince(o.since, now)
		if perr != nil {
			return nil, perr
		}
		q.LastUpdatedFrom = from
		q.LastUpdatedTo = now
	}
	if o.starting != "" {
		from, to, perr := parseStartingRange(o.starting, now)
		if perr != nil {
			return nil, perr
		}
		q.StartFrom = from
		q.StartTo = to
	}

	// クライアント側フィルタ（status は API 側で選択済みのため除く）。
	eventTypeRe, err := query.CompileEventType(o.eventType)
	if err != nil {
		return nil, err
	}
	filter := query.Filter{
		Service:   o.service,
		Category:  o.category,
		Region:    o.region,
		EventType: eventTypeRe,
	}

	cfg, err := awsx.LoadConfig(ctx, o.profile)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	ca, err := cache.New(!o.noCache, o.refresh)
	if err != nil {
		return nil, fmt.Errorf("init cache: %w", err)
	}
	ttl, err := query.ParseDuration(o.cacheTTL)
	if err != nil {
		return nil, fmt.Errorf("--cache-ttl: %w", err)
	}
	ns := o.profile + "|" + awsx.HealthRegion

	logf := func(format string, a ...any) {
		if !quiet {
			fmt.Fprintf(os.Stderr, format, a...)
		}
	}

	logf("=== AWS Health Dashboard ===\nProfile: %s  Scope: %s  Status: %s%s  TZ: %s\n\n",
		orDefault(o.profile), o.scope, strings.Join(statuses, ","), modeSuffix(o), render.ZoneName())
	logf("Fetching events...\n")

	client := health.New(cfg, ca, ns, ttl)
	events, err := client.FetchEvents(ctx, org, q)
	if err != nil {
		return nil, fmt.Errorf("fetch events: %w", err)
	}
	logf("Found %d raw event(s)\n", len(events))

	events = filter.Apply(events)

	// open の鮮度フィルタ（既定 90d）。--since/--starting の明示時間モードでは適用しない。
	if o.since == "" && o.starting == "" && o.openSince != "0" {
		d, derr := query.ParseDuration(o.openSince)
		if derr != nil {
			return nil, fmt.Errorf("--open-since: %w", derr)
		}
		events = query.PruneStaleOpen(events, now, d)
	}

	// 前方ホライズン（--within）。
	if o.within != "" {
		d, derr := query.ParseDuration(o.within)
		if derr != nil {
			return nil, derr
		}
		events = query.ApplyHorizon(events, now, d)
	}
	logf("After filtering: %d event(s)\n", len(events))

	var logical []model.LogicalEvent
	if o.noMerge {
		logical = merge.NoMerge(events)
	} else {
		logical = merge.ByEventType(events)
		if len(events) > 0 {
			logf("Merged into %d logical event(s)\n", len(logical))
		}
	}

	// アクション優先度（status）→ 開始の近い順 に並べ替え。
	query.SortLogical(logical)

	return &fetchResult{
		client:  client,
		org:     org,
		logical: logical,
		now:     now,
		cache:   ca,
		ns:      ns,
		cfg:     cfg,
	}, nil
}

// parseLocation は --tz をタイムゾーンに解決する（""/utc=UTC, local=ローカル, それ以外は IANA 名）。
func parseLocation(tz string) (*time.Location, error) {
	switch strings.ToLower(strings.TrimSpace(tz)) {
	case "", "utc":
		return time.UTC, nil
	case "local":
		return time.Local, nil
	default:
		return time.LoadLocation(tz)
	}
}

func runEvents(ctx context.Context, o *eventsOpts) error {
	loc, lerr := parseLocation(o.tz)
	if lerr != nil {
		return fmt.Errorf("--tz %q: %w", o.tz, lerr)
	}
	render.SetDisplayLocation(loc)

	res, err := loadLogical(ctx, o, o.tui)
	if err != nil {
		return err
	}

	// 対話的 TUI: 一覧→Enter で詳細/影響リソースにドリルダウン。
	if o.tui {
		return tui.Run(ctx, &tui.Input{
			Client:  res.client,
			Org:     res.org,
			Events:  res.logical,
			Now:     res.now,
			Cache:   res.cache,
			NS:      res.ns,
			Cfg:     res.cfg,
			GroupBy: o.groupBy,
		})
	}

	client := res.client
	org := res.org
	logical := res.logical
	now := res.now
	ca := res.cache
	ns := res.ns
	cfg := res.cfg
	if len(logical) == 0 {
		return nil
	}

	// 詳細（説明＋eventMetadata）。--show-details か topic グルーピング時に取得。
	if o.showDetails || o.groupBy == "topic" {
		fmt.Fprintln(os.Stderr, "Fetching event details (description + metadata)...")
		if derr := enrich.Details(ctx, client, org, logical); derr != nil {
			fmt.Fprintf(os.Stderr, "warning: 詳細取得を一部スキップ: %v\n", derr)
		}
	}

	// 影響リソースの取得とアカウント名解決（--show-resources）。
	if o.showRes {
		fmt.Fprintln(os.Stderr, "Fetching affected resources...")
		if err := enrich.Resources(ctx, client, org, logical); err != nil {
			return fmt.Errorf("fetch affected resources: %w", err)
		}
		if org {
			names, nerr := cache.Fetch(ca, ns+"|accounts", cache.AccountsTTL, func() (map[string]string, error) {
				return orgs.New(cfg).NameMap(ctx)
			})
			if nerr != nil {
				fmt.Fprintf(os.Stderr, "warning: アカウント名解決をスキップ: %v\n", nerr)
			} else {
				enrich.ApplyAccountNames(logical, names)
			}
		}
	}
	if !o.noCache {
		fmt.Fprintf(os.Stderr, "Cache: %d hit / %d miss (API calls), TTL %s\n", ca.Hits(), ca.Misses(), o.cacheTTL)
	}
	fmt.Fprintln(os.Stderr)

	// 出力先（ファイル or 標準出力）。
	out := os.Stdout
	if o.output != "" {
		fh, ferr := os.Create(o.output)
		if ferr != nil {
			return fmt.Errorf("create output file: %w", ferr)
		}
		defer fh.Close()
		out = fh
	}
	switch o.groupBy {
	case "type", "topic":
		var groups []model.EventGroup
		if o.groupBy == "topic" {
			groups = group.ByTopic(logical)
		} else {
			groups = group.ByEventType(logical)
		}
		fmt.Fprintf(os.Stderr, "Rolled up into %d %s group(s)\n", len(groups), o.groupBy)
		if err := render.RenderGroups(out, o.format, groups, now, o.groupBy == "topic", o.showDetails, o.showRes, o.showOcc); err != nil {
			return err
		}
	default:
		if err := render.Render(out, o.format, logical, now, o.showDetails, o.showRes); err != nil {
			return err
		}
	}
	if o.output != "" {
		fmt.Fprintf(os.Stderr, "Output saved to: %s\n", o.output)
	}
	return nil
}

func orDefault(s string) string {
	if s == "" {
		return "<default>"
	}
	return s
}

// modeSuffix はヘッダに付ける選択モードの補足。
func modeSuffix(o *eventsOpts) string {
	var parts []string
	if o.within != "" {
		parts = append(parts, "within "+o.within)
	}
	if o.since != "" {
		parts = append(parts, "since "+o.since)
	}
	if o.starting != "" {
		parts = append(parts, "starting "+o.starting)
	}
	if o.since == "" && o.starting == "" && o.openSince != "0" {
		parts = append(parts, "open≤"+o.openSince)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  (" + strings.Join(parts, ", ") + ")"
}
