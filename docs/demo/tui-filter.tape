# phd TUI デモ（VHS）— 絞り込み: status / category ピッカー、svc: チップ、フリーワード。
# 再生成: `make demo`（または `vhs docs/demo/tui-filter.tape`）。AWS 認証は不要。
Output docs/demo/tui-filter.gif

Set Shell "bash"
Set FontSize 14
Set Width 1500
Set Height 760
Set Padding 16

# TUI 起動
Type "./phd --demo docs/demo/fixture.json 2>/dev/null" Sleep 500ms Enter
Sleep 1800ms

# 1) status ピッカー（s）: upcoming だけに絞る（space で ON → enter で適用 → st: チップ）
Type "s" Sleep 1200ms
Down Sleep 600ms
Space Sleep 700ms
Enter Sleep 2200ms
Escape Sleep 1200ms

# 2) category ピッカー（c）: scheduledChange だけに絞る
Type "c" Sleep 1200ms
Down Sleep 350ms
Down Sleep 600ms
Space Sleep 700ms
Enter Sleep 2200ms
Escape Sleep 1200ms

# 3) サービス絞り込み（/ → svc:RDS → チップ化）
Type "/" Sleep 700ms
Type "svc:RDS" Sleep 900ms
Enter Sleep 2200ms
Escape Sleep 1200ms

# 4) フリーワード絞り込み（/ → lambda）
Type "/" Sleep 700ms
Type "lambda" Sleep 900ms
Enter Sleep 2200ms
Escape Sleep 1200ms

# q で終了
Type "q" Sleep 800ms
