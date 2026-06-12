# デモ（フェイクデータ）の作り方

`phd` の使い方をスクリーンショット・動画で見せるための仕組み一式。実データ（アカウント ID・ARN・影響リソース・
会社名）を共有せずに、**匿名化したフェイクデータ**で CLI / TUI をそのまま動かして録画する。

- ショーケース GIF は [トップ README のデモ節](../../README.md#デモスクショ動画) に掲載。
- このディレクトリの中身:
  - `fixture.json` … 同梱のサンプル（手書きのフェイクデータ・安全にコミット可）
  - `cli.tape` / `tui.tape` / `tui-filter.tape` … [VHS](https://github.com/charmbracelet/vhs) の録画スクリプト
  - `cli.gif` / `tui.gif` / `tui-filter.gif` … 生成物

---

## 1. デモモードで動かす（オフライン）

`--demo <fixture.json>` を渡すと **AWS を一切呼ばない**（SSO・ネットワーク不要）。fixture を読むのは取得層だけで、
`loadLogical` 以降（filter / prune / horizon / merge / sort / group）は実データと同じ経路を通る。
このため **1 つの fixture でフラグ違いのデモが全部撮れる**。

```bash
./phd --demo docs/demo/fixture.json                                   # 端末なら TUI
./phd --demo docs/demo/fixture.json --mode cli                        # 表で出力
./phd --demo docs/demo/fixture.json --mode cli --group-by type --show-resources
./phd --demo docs/demo/fixture.json --mode cli --service RDS --within 30d
```

録画時期がずれても open/upcoming の鮮度が保たれるよう、ロード時に fixture の時刻を `now` 基準へ自動リベースする
（`capturedAt` フィールドからの相対シフト）。

## 2. 自分の実データから fixture を作る（匿名化つき）

`--dump-fixture` は実 AWS から 1 回取得し、**その場で匿名化**して fixture を書き出す（PII を素のままディスクに残さない）。

```bash
# 要 SSO ログイン・1 回だけ。--show-resources を付けると影響リソースも取り込む。
./phd --profile my-sso-profile --show-resources \
  --dump-fixture /tmp/phd-fixture.json --scrub \
  --scrub-replace 'Acme=Globex' --scrub-replace 'acme.example.com=demo.example.com'

# できた fixture で完全オフライン再生
./phd --demo /tmp/phd-fixture.json
```

`--scrub` の決定論的な匿名化:

| 対象 | 置換後 |
| --- | --- |
| アカウント ID（12 桁） | `1000000000NN`（出現順に採番、全フィールド横断で一貫） |
| 影響リソース値 | プレフィックス保持で連番（`i-...`→`i-0demoNNNNNN`、ほか→`demo-res-NNN`） |
| アカウント名 | `Demo Account NN` |
| 任意トークン（会社名・ドメイン等） | `--scrub-replace old=new`（反復指定可） |

`eventTypeCode` / `service` / `region` / 時系列は**保持**する（リアルさを残すため）。

> **PII 注意**: コミットするのは **scrub 済み fixture のみ**。素の `--dump-fixture`（`--scrub` なし）は PII を含み得るので、
> 出力先は `/tmp` 等にして誤コミットを防ぐ。`/demo/` は `.gitignore` 対象（このサンプルは `docs/demo/` 配下なので追跡される）。

## 3. GIF を再生成する

[VHS](https://github.com/charmbracelet/vhs) が必要（`brew install vhs`）。

```bash
make demo   # docs/demo/*.tape → docs/demo/*.gif（cli / tui / tui-filter）
# 個別に: vhs docs/demo/cli.tape
```

### tape の編集メモ

- 表は横に広いので、CLI tape では端末桁数を確保する（`Set Width` / `Set FontSize` ≒ cols = `Width / FontSize`）。
  桁が足りないと列が折り返して読みにくくなる。
- 進捗ログ（`Config:` 行など）は `stderr` に出るため、各コマンドを `2>/dev/null` で実行して表だけを綺麗に見せる。
- TUI のキー操作（抜粋）: `↑/↓` 移動、`Enter` ドリルダウン、`Esc` 戻る/フィルタ解除、`s` status、`c` category、
  `/` フィルタ入力（`svc:` `cat:` `st:` `reg:` はチップ化／ほかは自由語）、`g` グループ切替、リソース画面で `a` resolved 表示切替、`q` 終了。

## fixture の形式

`internal/health/fixture.go` の `Fixture` 型の JSON。ARN をキーに各取得経路の応答を持つ。

```jsonc
{
  "capturedAt": "2026-06-10T00:00:00Z",      // 時刻リベースの基準
  "events":   [ /* 生イベント（merge 前の model.Event）*/ ],
  "details":  { "<arn>": { "description": "...", "metadata": { } } },
  "resources":{ "<arn>": [ { "accountId", "accountName", "region", "value", "status" } ] },
  "affectedAccounts": { "<arn>": ["<accountId>", ...] },
  "accountNames":     { "<accountId>": "<name>" }
}
```

resolved の表示/非表示トグルを見せたい場合は、`resources` に `"status": "RESOLVED"` の行を混ぜておく
（既定では隠れ、TUI のリソース画面で `a` を押すと表示される）。
