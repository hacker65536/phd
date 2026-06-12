# phd TUI デモ（VHS）— ドリルダウン＋影響リソースの resolved 表示トグル。
# 再生成: `make demo`（または `vhs docs/demo/tui.tape`）。AWS 認証は不要。
Output docs/demo/tui.gif

Set Shell "bash"
Set FontSize 14
Set Width 1500
Set Height 760
Set Padding 16

# TUI 起動（端末なので既定で TUI。stderr は隠す）
Type "./phd --demo docs/demo/fixture.json 2>/dev/null" Sleep 500ms Enter
Sleep 2s

# 一覧を EC2（影響リソースに resolved を含む）まで移動
Down Sleep 600ms
Down Sleep 600ms
Down Sleep 900ms

# Enter で詳細（説明 + eventMetadata）
Enter Sleep 2200ms

# Enter で影響リソース一覧（アカウント名つき・既定では resolved を隠す）
Enter Sleep 2500ms

# a で resolved を表示（"a: show/hide resolved"）→ もう一度 a で再び隠す
Type "a" Sleep 2500ms
Type "a" Sleep 1800ms

# Esc で戻る（リソース → 詳細 → 一覧）
Escape Sleep 1s
Escape Sleep 1s

# q で終了
Type "q" Sleep 800ms
