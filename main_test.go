package main

import "testing"

func TestNeedsEventsPrefix(t *testing.T) {
	root := rootCmd()
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"bare", nil, true},
		{"events explicit", []string{"events", "--within", "90d"}, false},
		{"events flag only", []string{"--within", "90d"}, true},
		{"events short flag", []string{"-p", "myprofile"}, true},
		{"version subcommand", []string{"version"}, false},
		{"help subcommand", []string{"help"}, false},
		{"version flag", []string{"--version"}, false},
		{"version short flag", []string{"-v"}, false},
		{"help flag", []string{"--help"}, false},
		{"help short flag", []string{"-h"}, false},
		{"completion", []string{"completion", "zsh"}, false},
		{"hidden complete", []string{"__complete", "--wi"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := needsEventsPrefix(root, c.args); got != c.want {
				t.Errorf("needsEventsPrefix(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestWantTUI(t *testing.T) {
	cases := []struct {
		name        string
		mode        string
		forceFormat bool
		stdoutTTY   bool
		want        bool
	}{
		// 端末・整形指定なし: mode に従う（既定 auto/tui は TUI）。
		{"auto interactive", "auto", false, true, true},
		{"tui interactive", "tui", false, true, true},
		{"cli interactive", "cli", false, true, false},

		// 非 TTY は常に CLI（スクリプト/パイプ保護）。tui 明示でも倒す。
		{"auto piped", "auto", false, false, false},
		{"tui piped", "tui", false, false, false},
		{"cli piped", "cli", false, false, false},

		// -f/-o 明示は CLI（端末でも）。tui 明示より優先。
		{"auto force-format", "auto", true, true, false},
		{"tui force-format", "tui", true, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wantTUI(c.mode, c.forceFormat, c.stdoutTTY); got != c.want {
				t.Errorf("wantTUI(%q, force=%v, tty=%v) = %v, want %v",
					c.mode, c.forceFormat, c.stdoutTTY, got, c.want)
			}
		})
	}
}
