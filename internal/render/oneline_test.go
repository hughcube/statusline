package render

import (
	"strings"
	"testing"

	"github.com/hughcube/statusline/internal/agent"
)

func TestOneLineFlattensControlCharacters(t *testing.T) {
	cases := map[string]string{
		"a\nb":       "a b",
		"a\r\n\tb":   "a b",
		"  a   b  ":  "a b",
		"a\x1b[31mb": "a [31mb",
		"\n\n":       "",
		"正常 文本\n第二行": "正常 文本 第二行",
	}
	for in, want := range cases {
		if got := OneLine(in); got != want {
			t.Errorf("OneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// CodeBuddy derives session_name from the first user message, so a multi-line
// prompt used to be pasted into line 1 and push the bar to three or more rows.
func TestBuildKeepsTwoRowsForMultiLineSession(t *testing.T) {
	f := &agent.Fields{
		Dir:       "C:/Users/hugh.li/Data/ms/statusline",
		Model:     "DeepSeek-V4.1-Flash",
		Version:   "2.181.0",
		Session:   "$ ai-codebuddy x-account list --refresh --travel\n  [1/6] Hugh (1765) 签到:已签\n每个账号都帮我去领取一个Buddy宠物",
		CtxSize:   1000000,
		InTokens:  1000,
		OutTokens: 200,
	}
	out := Build(f, "codebuddy", 134, Git{Branch: "main"})
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d rows, want 2: %q", len(lines), out)
	}
	for i, ln := range lines {
		if strings.ContainsAny(ln, "\r\t") {
			t.Errorf("row %d keeps a control character: %q", i+1, ln)
		}
	}
	if !strings.Contains(lines[1], "DeepSeek-V4.1-Flash") {
		t.Errorf("row 2 lost the model segment: %q", lines[1])
	}
}
