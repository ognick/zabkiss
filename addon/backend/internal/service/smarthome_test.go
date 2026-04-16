package service

import "testing"

// ── joinReplies ───────────────────────────────────────────────────────────────

func TestJoinReplies(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want string
	}{
		{
			name: "normal case — no trailing period",
			a:    "Свет включён",
			b:    "Музыка запущена",
			want: "Свет включён. Музыка запущена",
		},
		{
			name: "a ends with period — no double period",
			a:    "Свет включён.",
			b:    "Музыка запущена",
			want: "Свет включён. Музыка запущена",
		},
		{
			name: "a ends with period and space — no double period",
			a:    "Свет включён. ",
			b:    "Музыка запущена",
			want: "Свет включён. Музыка запущена",
		},
		{
			name: "a ends with multiple spaces and periods — still single period",
			a:    "Свет включён... ",
			b:    "Музыка запущена",
			want: "Свет включён. Музыка запущена",
		},
		{
			name: "b is empty string",
			a:    "Свет включён",
			b:    "",
			want: "Свет включён. ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := joinReplies(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("joinReplies(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// ── withOpenQuestion ──────────────────────────────────────────────────────────

func TestWithOpenQuestion(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  string
	}{
		{
			name:  "normal reply without question — appends Что ещё?",
			reply: "Свет выключен",
			want:  "Свет выключен Что ещё?",
		},
		{
			name:  "reply already ends with question mark — no change",
			reply: "Какую музыку включить?",
			want:  "Какую музыку включить?",
		},
		{
			name:  "reply with trailing spaces — spaces trimmed then Что ещё? appended",
			reply: "Свет выключен   ",
			want:  "Свет выключен Что ещё?",
		},
		{
			name:  "reply already ends with question mark and space — no change",
			reply: "Какую музыку включить? ",
			want:  "Какую музыку включить? ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := withOpenQuestion(tc.reply)
			if got != tc.want {
				t.Errorf("withOpenQuestion(%q) = %q, want %q", tc.reply, got, tc.want)
			}
		})
	}
}
