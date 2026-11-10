package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCalcUsed(t *testing.T) {
	cases := []struct {
		in   float64
		ok   bool
		want int
	}{
		{1, true, 0},
		{0, true, 100},
		{0.68, true, 32},
		{0.681448, true, 32},
		{0.9795087, true, 3},
		{0.99923146, true, 1},
		{0.4811144, true, 52},
		{0.5, true, 50},
		{0, false, -1},
	}
	for _, c := range cases {
		if got := CalcUsed(c.in, c.ok); got != c.want {
			t.Errorf("CalcUsed(%v,%v) = %d, want %d", c.in, c.ok, got, c.want)
		}
	}
}

func TestDetectAndParseCodeBuddy(t *testing.T) {
	raw := []byte(`{"hook_event_name":"Status","session_id":"s1","workspace":{"current_dir":"c:/a"},
	"model":{"display_name":"M"},"version":"1","context_window":{"context_window_size":1000,
	"current_usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":7},
	"used_percentage":59.4}}`)
	p := Parse(raw)
	a := Detect(p)
	if a.Name() != "codebuddy" {
		t.Fatalf("detect = %s", a.Name())
	}
	f := a.Parse(p)
	if f.InTokens != 10 || f.OutTokens != 5 || f.CacheRead != 7 {
		t.Errorf("token 提取错误: %+v", f)
	}
	if f.UsedPct == nil || *f.UsedPct != 59.4 {
		t.Errorf("used_percentage 未取到")
	}
}

func TestParseAntigravityQuotas(t *testing.T) {
	raw := []byte(`{"product":"antigravity","model":{"display_name":"G"},"version":"1","plan_tier":"Google AI Pro",
	"quota":{"3p-5h":{"remaining_fraction":0.68},"3p-weekly":{"remaining_fraction":0.85},
	"gemini-5h":{"remaining_fraction":0.64},"gemini-weekly":{"remaining_fraction":0.9},
	"gemini-argon-5h":{"remaining_fraction":0.8},"gemini-argon-wtu-weekly":{"remaining_fraction":0.5}}}`)
	p := Parse(raw)
	f := Antigravity{}.Parse(p)
	if len(f.Quotas) != 3 {
		t.Fatalf("配额组数 = %d, want 3 (%+v)", len(f.Quotas), f.Quotas)
	}
	if f.Quotas[0].Used5 != 32 || f.Quotas[0].Used7 != 15 {
		t.Errorf("P 组错误: %+v", f.Quotas[0])
	}
	if f.Quotas[1].Used5 != 36 || f.Quotas[1].Used7 != 10 {
		t.Errorf("G 组错误: %+v", f.Quotas[1])
	}
	if f.Quotas[2].Label != "G4" || f.Quotas[2].Used5 != 20 || f.Quotas[2].Used7 != 50 {
		t.Errorf("G4 组错误: %+v", f.Quotas[2])
	}
}

func TestParseClaudeRateLimits(t *testing.T) {
	raw := []byte(`{"workspace":{"current_dir":"/x"},"model":{"display_name":"Opus"},"version":"2.1.0",
	"rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1762473600},"seven_day":{"used_percentage":41.2,"resets_at":1762992000}}}`)
	p := Parse(raw)
	if Detect(p).Name() != "claude" {
		t.Fatalf("detect = %s", Detect(p).Name())
	}
	f := Claude{}.Parse(p)
	if len(f.Quotas) != 1 || f.Quotas[0].Used5 != 23 || f.Quotas[0].Used7 != 41 {
		t.Errorf("rate_limits 解析错误: %+v", f.Quotas)
	}
	if !f.Quotas[0].Reset5.Equal(time.Unix(1762473600, 0)) {
		t.Errorf("five_hour resets_at 解析错误: %v", f.Quotas[0].Reset5)
	}
	if !f.Quotas[0].Reset7.Equal(time.Unix(1762992000, 0)) {
		t.Errorf("seven_day resets_at 解析错误: %v", f.Quotas[0].Reset7)
	}
}

func TestAntigravityReadsAnnotationPbtxtDirectly(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("AI_APP_DATA_DIR", appData)

	sid := "ad7eec22-0aa0-490e-b7db-ce732dd01bf3"
	annotDir := filepath.Join(appData, "annotations")
	if err := os.MkdirAll(annotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pbtxtFile := filepath.Join(annotDir, sid+".pbtxt")
	if err := os.WriteFile(pbtxtFile, []byte("title:\"Fix Statusline Error\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{
		"product": "antigravity",
		"session_id": "` + sid + `",
		"conversation_id": "` + sid + `",
		"model": {"display_name": "Gemini 3.8 Flash"}
	}`)
	p := Parse(payload)
	f := Antigravity{}.Parse(p)
	if f.Session != "Fix Statusline Error" {
		t.Fatalf("Session = %q, want %q", f.Session, "Fix Statusline Error")
	}
	if f.SessionID != sid {
		t.Fatalf("SessionID = %q, want %q", f.SessionID, sid)
	}
}

func TestAntigravityFallbackIgnoresRawUUID(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("AI_APP_DATA_DIR", appData)

	sid := "ad7eec22-0aa0-490e-b7db-ce732dd01bf3"
	payload := []byte(`{
		"product": "antigravity",
		"session_id": "` + sid + `",
		"session_name": "` + sid + `",
		"model": {"display_name": "Gemini 3.8 Flash"}
	}`)
	p := Parse(payload)
	f := Antigravity{}.Parse(p)
	if f.Session != "" {
		t.Fatalf("Session should be empty when no title and session_name is UUID, got %q", f.Session)
	}
}

func TestAntigravityCacheReadCurrentUsage(t *testing.T) {
	payload := []byte(`{
		"product": "antigravity",
		"session_id": "test-sid",
		"context_window": {
			"total_input_tokens": 63187,
			"total_output_tokens": 115449,
			"context_window_size": 1048576,
			"current_usage": {
				"input_tokens": 3215,
				"output_tokens": 969,
				"cache_creation_input_tokens": 120,
				"cache_read_input_tokens": 64709
			}
		}
	}`)
	p := Parse(payload)
	f := Antigravity{}.Parse(p)
	if f.InTokens != 63187 {
		t.Fatalf("InTokens = %d, want 63187", f.InTokens)
	}
	if f.OutTokens != 115449 {
		t.Fatalf("OutTokens = %d, want 115449", f.OutTokens)
	}
	if f.CacheRead != 64709 {
		t.Fatalf("CacheRead = %d, want 64709", f.CacheRead)
	}
	if f.CacheCreate != 120 {
		t.Fatalf("CacheCreate = %d, want 120", f.CacheCreate)
	}
}

func TestClaudeDetectWithEmptyQuota(t *testing.T) {
	payload := []byte(`{
		"workspace": {"current_dir": "/path/to/project"},
		"model": {"display_name": "Claude 3.7 Sonnet"},
		"quota": {}
	}`)
	p := Parse(payload)
	a := Detect(p)
	if a.Name() != "claude" {
		t.Fatalf("Detect with empty quota = %s, want claude", a.Name())
	}
}

func TestClaudeTokensFallbackAndThinking(t *testing.T) {
	payload := []byte(`{
		"workspace": {"current_dir": "/path/to/project"},
		"model": {"display_name": "Claude 3.7 Sonnet"},
		"context_window": {
			"input_tokens": 1234,
			"output_tokens": 567
		},
		"thinking": {"enabled": true},
		"fast_mode": true
	}`)
	p := Parse(payload)
	f := Claude{}.Parse(p)
	if f.InTokens != 1234 {
		t.Fatalf("InTokens = %d, want 1234", f.InTokens)
	}
	if f.OutTokens != 567 {
		t.Fatalf("OutTokens = %d, want 567", f.OutTokens)
	}
	if f.Think != "think" {
		t.Fatalf("Think = %s, want think", f.Think)
	}
	if !f.Fast {
		t.Fatalf("Fast = false, want true")
	}
}
