package render

import (
	"regexp"
	"strings"
)

// ansiRe は ANSI エスケープシーケンス（CSI / OSC）と 2 バイトエスケープを表す。
// AWS Health 由来の文字列でもアカウント名・リソース値・説明はユーザーが任意に決められるため、
// 端末へ出力する前にこれらを除去し、出力偽装・カーソル/画面操作・タイトル書き換えを防ぐ。
// 取りこぼした単独 ESC は scrubControl のループ側で空白へ落とす（多層防御）。
var ansiRe = regexp.MustCompile(
	`\x1b\[[0-9;:?]*[ -/]*[@-~]` + // CSI（色・カーソル移動など）
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC（タイトル等）… BEL または ST 終端
		`|\x1b[@-_]`) // その他 2 バイトエスケープ

// scrubControl は ANSI シーケンスを除去し、残った C0 制御文字 / DEL を無害化する。
// keepNewline=true は改行・タブを保持（複数行の説明文用）、false は空白へ置換して
// 1 行セルのレイアウトを守る。CR は端末で行頭復帰＝上書きに使えるため常に落とす。
func scrubControl(s string, keepNewline bool) string {
	if s == "" {
		return s
	}
	s = ansiRe.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			if keepNewline {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		case r == '\r':
			if !keepNewline {
				b.WriteByte(' ')
			}
		case r < 0x20 || r == 0x7f:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SanitizeCell は 1 行セル向け。ANSI を除去し、改行・タブ・CR を含むすべての制御文字を
// 空白化してテーブル/CSV のレイアウト崩れと端末インジェクションを防ぐ。
func SanitizeCell(s string) string { return scrubControl(s, false) }

// SanitizeText は複数行ブロック向け。改行・タブは保持しつつ ANSI と他の制御文字を除去する。
func SanitizeText(s string) string { return scrubControl(s, true) }
