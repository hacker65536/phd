# phd CLI デモ（VHS）。再生成: `make demo`（または `vhs docs/demo/cli.tape`）。
# フェイクデータ docs/demo/fixture.json から起動するため AWS 認証は不要。
# 進捗ログ（stderr）は 2>/dev/null で隠し、表だけを綺麗に見せる。
# 表は横に広いので端末桁数を確保する（cols ≈ Width / FontSize）。
Output docs/demo/cli.gif

Set Shell "bash"
Set FontSize 13
Set Width 1900
Set Height 720
Set Padding 16

# 1) 既定の一覧（region をまたいだ同一イベントが 1 行に束ねられる）
Type "./phd --demo docs/demo/fixture.json --mode cli 2>/dev/null" Sleep 500ms Enter
Sleep 3s

# 2) eventTypeCode 単位でロールアップ
Type "./phd --demo docs/demo/fixture.json --mode cli --group-by type 2>/dev/null" Sleep 500ms Enter
Sleep 3s

# 3) 影響リソースを全アカウント・全リージョン分まとめて展開
Type "./phd --demo docs/demo/fixture.json --mode cli --show-resources 2>/dev/null" Sleep 500ms Enter
Sleep 4s

Sleep 1s
