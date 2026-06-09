# phd — AWS Health Dashboard CLI

`phd`（Personal Health Dashboard 由来）は、AWS Health のイベントをローカルの CLI / TUI で確認するための Go ツール。
AWS マネジメントコンソールでは煩雑な「期間・サービス・イベント単位の絞り込み」と「region をまたぐ同一イベントの集約」「影響リソースの横断確認」を、1 コマンドで行うことを目的とする。

---

## ビルド・実行・テスト

```bash
# ビルド（バイナリ名は phd）
go build -o phd .

# 実行（既定: organization スコープ、status=open,upcoming を開始の近い順）
./phd events

# 対話的 TUI（一覧 → Enter で詳細 → 影響リソースにドリルダウン）
./phd events --tui

# ユニットテスト
go test ./...

# 実 AWS を使った統合テスト（要 SSO ログイン）
bash scripts/smoke.sh
```

AWS SSO プロファイルを指定して実行する例（`~/.aws/config` のプロファイルを使用）:

```bash
./phd events --profile my-sso-profile
# 別の config ファイルを使う場合のみ:
#   export AWS_CONFIG_FILE=$HOME/.aws/my-config
```

---

## アーキテクチャ

単一コマンド `phd events`（`main.go`）。取得パイプラインの前半（fetch → filter → prune → horizon → merge → sort）は `loadLogical` に集約し、**CLI と TUI が同じ経路を共有**する。enrich（詳細・影響リソース取得）は遅延実行。

| パッケージ | 役割 |
| --- | --- |
| `internal/awsx` | AWS config ロード（Health は us-east-1 固定） |
| `internal/health` | Health API クライアント（イベント / 詳細 / 影響リソース取得、10 アカウントずつバッチ） |
| `internal/orgs` | Organizations のアカウント名解決 |
| `internal/query` | 取得条件・クライアント側フィルタ・期間/ソート |
| `internal/merge` | region をまたぐ同一 eventTypeCode を 1 論理イベントに集約 |
| `internal/group` | `--group-by type`（eventTypeCode）/ `topic`（eventMetadata）のロールアップ |
| `internal/enrich` | 詳細・影響リソース・アカウント名の付与 |
| `internal/model` | `LogicalEvent` / `EventGroup` 等のドメイン型 |
| `internal/cache` | ローカルキャッシュ（`~/Library/Caches/phd`） |
| `internal/render` | table / json / csv / markdown 出力 |
| `internal/tui` | Bubble Tea ベースの対話的 TUI |

### 抽象化の 4 層
生イベント → occurrence`(eventTypeCode, startTime)` → `--group-by type` → `--group-by topic`。`--show-occurrences` で配下を展開。

### 確定済みの設計判断（再議論不要）
- 既定はステータス主軸（時間で切らない＝upcoming 取りこぼし防止）。期間は `--within` / `--since` / `--starting` で明示。
- occurrence のマージキーは `(eventTypeCode, startTime)`。別日程は分離する。
- 既定は全カテゴリ統合（コンソールはタブ別、`--category` で再現）。
- `DescribeAffectedEntitiesForOrganization` は 10 アカウントずつバッチ。

---

## 設定（Viper）

`config.yaml` を `./` と `~/.config/phd/` から自動探索（`--config` で明示も可）。
優先順位は **フラグ > 環境変数 `PHD_*` > config > 既定**。`service` / `category` / `filter-region` / `status` はカンマ区切り（または YAML リスト）で複数指定可。

- `config.yaml` … 実設定（payer アカウント ID 入りのため **git 追跡しない**）
- `config.example.yaml` … テンプレート（追跡する）

---

## このリポジトリで作業するときの注意

- AWS API は aws-sdk-go-v2 を直叩きする。レスポンスのキャッシュは `internal/cache`（`~/Library/Caches/phd`、既定 TTL 1h）で自前実装。
- 時刻は既定 UTC。`--tz`（`local` / `Asia/Tokyo` 等）で表示変換。IANA tzdata はバイナリに埋め込み済み。
