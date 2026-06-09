# phd

[![CI](https://github.com/hacker65536/phd/actions/workflows/ci.yml/badge.svg)](https://github.com/hacker65536/phd/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hacker65536/phd)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

AWS Health Dashboard をローカル CLI で確認するツール（Go 製）。
AWS マネジメントコンソールの不満を解消することを目的としている。

- **region をまたいで同一イベントを束ねる** — コンソールは region 最前段で同じイベントが分裂するが、`eventTypeCode` 単位で1つの論理イベントに集約する。
- **影響リソースをワンショットで平坦表示** — 「event → 影響リソース → アカウントID → リスト」の多クリックを排除し、全アカウント・全リージョンのリソースを1テーブルに展開する。
- **アカウント名で表示** — `Organizations:ListAccounts` で ID→名前 を解決し「名前 (ID)」で表示。
- **org 横断** — メンバーアカウントを1つずつ見て回らず、管理アカウントから全社を一括取得。
- **EOL/廃止予告に `EOL` ラベル** — TUI 一覧で planned-lifecycle 系（`AWS_*_PLANNED_LIFECYCLE_EVENT`）にオレンジの `EOL` ラベルを付与。eventTypeCode だけで判定するため**詳細取得は不要**。`/` で `eol` と打てば EOL 系だけ抽出できる。
- **強力な絞り込み** — 期間・サービス・カテゴリ・ステータス・イベント種別（正規表現）・リージョン。

## ビルド

```bash
go build -o phd .
go test ./...        # AWS 不要のユニットテスト（merge / query / render）
```

## 前提

- AWS Health API は **us-east-1 固定**（本ツールが自動設定）。
- **Business / Enterprise サポートプラン**が必要。
- org スコープは **Organizations 管理アカウント**で実行し、Health の Organizational View が有効であること。
  名前解決には `organizations:ListAccounts` 権限が要る（読み取りのみ）。

## 抽象化の4層

イベントは段階的に束ねられる。視覚的なノイズを減らすほど上の層を使う。

1. **生イベント** — AWS が返す (service, eventTypeCode, region, startTime) 単位。イベントの一意キーは ARN。
2. **occurrence（既定の表示単位）** — `(eventTypeCode, startTime)` で region コピーを束ねたもの。同一スケジュールの複数リージョンが1行になる。
3. **type（`--group-by type`）** — `eventTypeCode` 単位のロールアップ。例: `AWS_LAMBDA_PLANNED_LIFECYCLE_EVENT` が全部1行（Python3.9 EOL も Node18 EOL も混ざる＝粗い）。
4. **topic（`--group-by topic`）** — `eventTypeCode + eventMetadata` 単位。**「Python 3.9 EOL」「Node.js 16 廃止」など話題ごとに1行**。同じ話題が region/日程に散っていても束ねる。最も意味のある粒度。

実例（全社）: occurrence 299 → type 54 → topic 67。`AWS_LAMBDA_RUNAWAY_TERMINATION_NOTIFICATION` の104件は1行に、Lambda の lifecycle は廃止ランタイムごとに分かれる。

```bash
# 話題ごとに束ねる（最も読みやすい最上位ビュー）
phd events --group-by topic

# eventTypeCode 単位の粗いロールアップ
phd events --group-by type

# ファミリー → 配下の日程(occurrence)に展開
phd events --group-by topic --service LAMBDA --show-occurrences

# ファミリー配下の全影響リソースを平坦表示（アカウント名付き）
phd events --group-by topic --service EKS --status upcoming --show-resources
```

要約表の `STATUS` は occurrence の件数内訳（`open:2 upcoming:5`）、`NEXT` は直近の予定開始までの残り、`OCC` は配下 occurrence 数、`TOPIC`/`EVENT_TYPE` がファミリー名。

> **メモ**: topic は各イベントの `eventMetadata`（`DescribeEventDetails`）を使う。実データでは planned-lifecycle 系（EOL/廃止予告）が一貫して `deprecated_versions` キーを持ち、値は「AWS Lambda end of support for Python 3.9」のような話題タイトル。metadata が無いイベントは eventTypeCode にフォールバックする。`--group-by topic` は全イベントの詳細取得を伴うため初回は遅い（キャッシュ後は高速）。
>
> 詳細取得は Health の低い TPS 上限（429）に配慮し、**`DescribeEventDetails[ForOrganization]` を最大 10 件ずつバッチ**で叩き、**SDK アダプティブリトライ＋クライアント側レートリミット**で送信を平準化する。`DescribeAffectedAccountsForOrganization` は詳細・影響リソースで共有キャッシュし、org 詳細では `eventScopeCode=ACCOUNT_SPECIFIC` のときだけ呼ぶ。

## 対話的ドリルダウン（TUI）

`--show-occurrences` / `--show-resources` / `--show-details` は静的展開（毎回フラグを変えて再実行）だが、`--tui` を付けると対話的に潜れる。コンソールの「event → リソース → アカウント → リスト」の多クリックを、一覧から Enter 一発に置き換える。

```bash
# 既定（occurrence 一覧）から対話的にドリルダウン
phd events --tui

# group（type/topic）一覧をトップにして group → occurrence → 詳細 と3段で潜る
phd events --tui --group-by type
```

- **3ページ構成**: 一覧 → `Enter` で**詳細**（メタ＋説明 latestDescription）→ さらに `Enter` で**影響リソース一覧**（独立ページ）。影響リソースは**アカウント順にソート**し、**既定で RESOLVED を非表示**（`a` で全表示トグル、RESOLVED 行は薄色）。影響リソース一覧で `e` を押すと、**現在表示中のリソースを CSV にエクスポート**（カレントに `phd-resources-<timestamp>.csv` を自動命名で保存。`a` で表示を切り替えた内容がそのまま反映される）。詳細の見出し直下には eventMetadata の人間可読な説明（例「AWS Lambda end of support for Python 3.9」）を表示。詳細・リソースは入場時に遅延ロード（裏で `DescribeEventDetails` / `DescribeAffectedEntities`）。
- **Esc / Backspace** で1階層戻る（カーソル位置は復元）。`/` でインクリメンタル絞り込み、`q` で終了。
- 取得条件は CLI と同じ（`--status` `--within` `--service` `--scope` など全フラグが効く）。`--tui` 時は `--format` / `--output` は無視。
- リソースは初回だけ API を叩き、2回目以降はキャッシュで即時。org スコープではアカウント名（`名前 (ID)`）も解決する。

| キー | 動作 |
|---|---|
| `↑`/`↓`, `j`/`k` | 一覧/詳細の移動・スクロール |
| `Enter`, `l` | 1階層下へ（一覧→詳細→影響リソース一覧） |
| `Esc`, `Backspace`, `h` | 1階層戻る |
| `g` | （一覧で）グループ表示を切替（none → type → topic を循環） |
| `r` | （詳細/影響リソースで）キャッシュを無視して現在のイベントを再取得 |
| `a` | （影響リソース一覧で）RESOLVED の表示/非表示を切替 |
| `e` | CSV エクスポート（一覧では表示中のイベント、影響リソース一覧では表示中のリソース。カレントに自動命名で保存） |
| `/` | 一覧の絞り込み（下記の構文） |
| `c` | category チェックボックス選択（4値固定 enum） |
| `s` | status チェックボックス選択（open/upcoming/closed） |
| `q`, `Ctrl-C` | 終了 |

### category / status のチェックボックス選択（`c` / `s`）

値が固定 enum の軸は、`/` で打たずにチェックボックスで選べる。`c`（category: issue / accountNotification / scheduledChange / investigation）または `s`（status: open / upcoming / closed）でパネルを開き、`space` で ON/OFF、`Enter` で適用。選択は内部的に `cat:` / `st:` チップ（カンマ区切り OR）になり、`filters:` 行に表示される。各行には現在データ内の件数も出る。

> **注**: status は TUI が**既に取得済みのステータス範囲内**でのクライアント側フィルタ。既定取得は `open,upcoming` なので、`closed` を選んでも取得していなければ 0 件。closed も見たい場合は起動時に `--status open,upcoming,closed` か `--since` を付ける。

### 絞り込み構文（`/`）

空白区切りトークンの **AND**。各トークンはプレフィックスで軸を指定でき、プレフィックス無しは全フィールド横断の部分一致（大小無視）。

| トークン | 対象 | 例 |
|---|---|---|
| `svc:` / `service:` | サービス | `svc:RDS` |
| `cat:` / `category:` | カテゴリ | `cat:scheduledChange` |
| `st:` / `status:` | ステータス | `st:upcoming` |
| `reg:` / `region:` | リージョン | `reg:ap-northeast-1` |
| `type:` / `et:` | eventTypeCode（topicラベル含む）部分一致 | `type:UPGRADE` |
| `re:` | eventTypeCode への正規表現（大小無視） | `re:^AWS_LAMBDA` |
| （プレフィックス無し） | 全フィールド部分一致 | `eks` `AWS_RDS_ENGINE_UPGRADE` |

`svc:` `cat:` `st:` `reg:` `type:` の値は**カンマ区切りで OR** 指定できる（スペースは入れない）。例: `svc:RDS,LAMBDA`（RDS または LAMBDA）、`cat:scheduledChange,accountNotification`、`st:open,upcoming`。複数軸を並べると軸間は AND（例: `svc:RDS,EC2 st:upcoming`）。

例: `svc:RDS cat:scheduledChange` / `re:^AWS_EC2 st:upcoming` / `lambda ap-northeast-1`。
不正な正規表現（`re:`）は eventTypeCode への部分一致にフォールバックする。

フィルタは性質で 2 系統に分かれる（**faceted search**）。

フィルタが有効なときは一覧上部に **検索行（`search:`）＋チップ行（`filters:`）の 2 行**が固定表示され、`/` で開いても行が増えない（レイアウトがずれない）。

- **低カーディナリティの軸**（`svc:` `cat:` `st:` `reg:`）は、確定（`Enter`）すると **チップ**として `filters:` 行に残る（`filters: [svc:RDS] [cat:scheduledChange]  12/61`）。同じ軸を再指定すると置き換わる（1 軸 1 値）。`search:` 欄には残らない。
- **自由語検索**（`type:` `re:` やプレフィックス無しのワード）は **`search:` 欄に残る**。`/` で再オープンすると前回の自由語が復元され、続けて編集できる。

確定時、入力した `key:value` のうち上記 4 軸はチップへ昇格し、残りは自由語として入力欄に残る（例: `svc:EC2 retirement` → チップ `svc:EC2` ＋ 入力欄 `retirement`）。`Esc` は入力中なら取消（確定状態へ戻す）、確定後ならチップ・自由語を一括解除（さらに `Esc` で 1 階層戻る）。フィルタはドリルダウンして戻っても**維持される**（各階層ごとに保持・復元）。

## 設定ファイル（config.yaml）

全フラグは YAML / 環境変数でも指定できる（Viper）。毎回同じプロファイルや絞り込みを打たずに済む。

- 探索順（`--config` 未指定時）: `./config.yaml` → `~/.config/phd/config.yaml`
- `--config path/to.yaml` で明示指定も可
- 環境変数: `PHD_<FLAG>`（ダッシュは `_`）。例 `PHD_SERVICE=EKS`, `PHD_GROUP_BY=topic`
- **優先順位: コマンドラインフラグ > 環境変数 > config > 既定**
- キーはフラグ名と同じ。雛形は [`config.example.yaml`](config.example.yaml)

```yaml
# config.yaml
profile: "my-sso-profile"
scope: organization
status: upcoming
within: 120d
group-by: topic
cache-ttl: 6h
```

```bash
# config.yaml がカレントにあれば自動で読む
./phd events
# その場で一部だけ上書き（CLI が config に勝つ）
./phd events --service EKS --group-by type
```

## 期間の考え方（重要）

「期間」を `[from, to]` の単一窓で決めると必ず破綻する。upcoming は開始が未来、closed は過去、open は今をまたぐため、1本の軸に対称窓をかけると片側が切れる（例: `startTime` 上限を now にすると未来の予定が全部消える）。

そこで本ツールは **選択軸＝ステータス、時間軸＝表示/並べ替え**に分離している。

- **既定 = `upcoming` 全部 ＋ `open` は直近 `--open-since`（既定 90d）に更新されたもの**を取得し、`startTime`（変更が起きる時刻）の近い順に並べる。`IN` 列に開始までの残り時間を出す。これで「いつ・何が起きるか」が漏れなく、かつ長期間居座る古い通知のノイズを抑えて出る。
- `open` が多すぎる/少なすぎる場合は `--open-since 30d`（厳しく）/ `--open-since 0`（無効=全 open）で調整。「対応すべき予定」だけ見たいなら `--status upcoming`。
- 時間で絞りたいときは、意味の違う3つを使い分ける:
  - `--within 90d` … 前方ホライズン。開始が今後その期間内のものだけ（進行中は常に残す）。
  - `--since 30d` … 履歴モード。その期間に更新されたものを `closed` 含めて表示（このとき open 鮮度フィルタは無効）。
  - `--starting A..B` … 開始時刻レンジ（監査用、同上）。

## 使い方

```bash
# 既定: organization・open+upcoming・開始の近い順・region マージ・table
phd events --profile <管理アカウントの profile>

# 本当に対応が必要な「予定」だけを近い順に
phd events --status upcoming

# 今後90日に始まる予定だけに絞る
phd events --within 90d

# 何が変わるのか（説明文）＋ どのリソースが影響するか を一気に
phd events --service EKS --show-details --show-resources

# 痛点そのままの典型ユース: カテゴリ→影響リソースを全アカウント分展開
phd events --category scheduledChange --show-resources

# 履歴: 直近30日に更新された closed 含む全件
phd events --since 30d

# 監査: この期間に始まる事象だけ
phd events --starting 2026-06-01..2026-09-01

# 単一アカウント / region マージ無効 / 各種出力
phd events --scope account
phd events --no-merge
phd events --category scheduledChange --show-resources --format csv -o report.csv
```

### 主なフラグ

| フラグ | 説明 |
|---|---|
| `--scope organization\|account` | 既定 organization |
| `--status open,upcoming,closed` | 取得するステータス。既定 `open,upcoming`（`--since` 時は closed も） |
| `--open-since DUR` | open は直近 DUR に更新されたものだけ（既定 `90d`、`0` で無効）。`--since`/`--starting` 時は無視 |
| `--within DUR` | 前方ホライズン（開始が今後 DUR 内）。例 `90d` `2w` |
| `--since DUR` | 履歴: その期間に更新＋closed 含む。例 `30d` `YYYY-MM-DD` |
| `--starting A..B` | 開始時刻レンジ（監査）。例 `2026-06-01..2026-09-01` |
| `--service` `--category` `--filter-region` | 絞り込み（完全一致） |
| `--event-type REGEX` | `eventTypeCode` への正規表現 |
| `--group-by type\|topic` | ロールアップ。type=eventTypeCode 単位 / topic=話題(eventMetadata)単位 |
| `--show-occurrences` | `--group-by` 時: 各ファミリー配下の occurrence(日程) を展開 |
| `--show-details` | 変更内容の説明（latestDescription）を展開 |
| `--show-resources` | 影響リソースを全アカウント・全リージョン分、平坦テーブルで展開 |
| `--tui` | 対話的 TUI を起動（一覧→Enter で詳細/影響リソースにドリルダウン）。`--format`/`--output` は無視 |
| `--no-merge` | region マージ無効 |
| `-f, --format` | `table\|json\|csv\|markdown` |
| `-o, --output FILE` | ファイル出力 |
| `--no-cache` `--refresh` | キャッシュ無効 / 強制再取得 |
| `--cache-ttl DUR` | キャッシュ有効期間（既定 1h。例 30m/6h/1d） |

並びは **status（open→upcoming）→ startTime 昇順**（＝今動いているもの→開始が近い予定→先の予定）。
region マージのキーは **`(eventTypeCode, startTime)`** で、同一スケジュールの region コピーだけ束ね、別日程の occurrence は分離する。

## キャッシュ

API 応答を `~/Library/Caches/phd`（OS の UserCacheDir 配下）に TTL 付きで保存する。
- events / resources / details: 既定 **1 時間**（`--cache-ttl 30m|6h|1d` で変更）。
- アカウント名マップ: 24 時間。
- 命中時は API を一切叩かない（同一クエリ2回目は実測 0.01 秒）。`--service` などのクライアント側絞り込みは同じ events キャッシュを再利用する。

実行ごとに stderr へ `Cache: N hit / M miss (API calls), TTL 1h` を表示するので、API を叩いたか一目で分かる。`--refresh` で強制再取得、`--no-cache` で無効化。キャッシュ全消去は `rm -rf ~/Library/Caches/phd`。

## アーキテクチャ

`fetch → filter → merge → enrich → render` を縦に分離し、各層をユニットテスト可能にしている。

```
main.go                      cobra: events コマンド
internal/
  awsx/      AWS 設定（profile 解決 + region=us-east-1 固定）
  health/    取得層（account/org の events・affected resources を model に正規化）
  orgs/      ListAccounts による ID→名前 解決
  model/     SDK 非依存の型（Event / Resource / LogicalEvent）
  merge/     region マージ（eventTypeCode 単位）         ← 中心機能・テスト有
  query/     期間パース・絞り込み                          ← テスト有
  enrich/    影響リソースの並列取得・平坦化・名前付与
  render/    table / json / csv / markdown                ← テスト有
  tui/       対話的ドリルダウン（Bubble Tea）             ← テスト有
  cache/     sha256 キー + TTL のファイルキャッシュ
```

取得パイプライン前半（fetch→filter→merge→sort）は `loadLogical` に切り出し、CLI（静的レンダリング）と TUI（遅延ロード付きドリルダウン）が共有する。TUI の AWS 呼び出しは `internal/tui/load.go` の `tea.Cmd` 内に隔離されており、`Update` はメッセージ注入でユニットテストできる。

## ライセンス

[MIT License](LICENSE)
