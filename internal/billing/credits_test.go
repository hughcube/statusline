package billing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hughcube/statusline/internal/agent"
)

func TestFormatCredits(t *testing.T) {
	cases := map[float64]string{
		0:       "0",
		59:      "59",
		999:     "999",
		3359.22: "3,359",
		1234567: "1,234,567",
		-5:      "0",
	}
	for in, want := range cases {
		if got := formatCredits(in); got != want {
			t.Errorf("formatCredits(%v) = %q, want %q", in, got, want)
		}
	}
}

func creditAt(t *testing.T, stamp string) float64 {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04:05", stamp, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return float64(ts.Unix())
}

func TestParseCreditsExpiryWindow(t *testing.T) {
	now := time.Date(2026, 11, 7, 16, 0, 0, 0, time.Local)
	env := creditsEnvelope{
		Summary: json.RawMessage("{\"code\":0,\"data\":{\"TotalCredits\":3359.22}}"),
		Expiry: [][2]float64{
			{creditAt(t, "2026-11-08 10:41:12"), 59},  // inside the window
			{creditAt(t, "2026-11-09 10:44:05"), 100}, // 25h away
			{creditAt(t, "2026-11-07 15:00:00"), 30},  // already expired
			{creditAt(t, "2026-11-07 20:00:00"), 0},   // nothing left
		},
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := parseCredits(raw, now)
	if !ok {
		t.Fatal("expected a usable snapshot")
	}
	if c.Total != 3359.22 {
		t.Errorf("total = %v, want 3359.22", c.Total)
	}
	if c.Expiring != 59 {
		t.Errorf("expiring = %v, want 59", c.Expiring)
	}
}

func TestParseCreditsTotalsFallback(t *testing.T) {
	now := time.Now()
	env := creditsEnvelope{
		Summary: json.RawMessage("{\"code\":0,\"data\":{\"TotalCredits\":\"1200\",\"Packages\":[{\"PackageCode\":\"p1\",\"CycleRemainCapacity\":\"1200\"}]}}"),
	}
	raw, _ := json.Marshal(env)
	c, ok := parseCredits(raw, now)
	if !ok || c.Total != 1200 || c.Expiring != 0 {
		t.Fatalf("unexpected snapshot: %+v ok=%v", c, ok)
	}
}

func TestParseCreditsRejectsBrokenPayload(t *testing.T) {
	if _, ok := parseCredits(json.RawMessage("{\"summary\":{\"code\":7}}"), time.Now()); ok {
		t.Fatal("a non-zero code must not be treated as a snapshot")
	}
	if _, ok := parseCredits(nil, time.Now()); ok {
		t.Fatal("an empty cache must not be treated as a snapshot")
	}
}

func TestExpiryPointsFiltersInactive(t *testing.T) {
	raw := json.RawMessage("{\"code\":0,\"data\":{\"Accounts\":[" +
		"{\"Status\":0,\"CycleCapacityRemain\":59,\"CycleEndTime\":\"2026-11-08 10:41:12\"}," +
		"{\"Status\":1,\"CycleCapacityRemain\":40,\"CycleEndTime\":\"2026-11-08 11:00:00\"}," +
		"{\"Status\":0,\"CycleCapacityRemain\":0,\"CycleEndTime\":\"2026-11-08 12:00:00\"}]}}")
	points := expiryPoints(raw)
	if len(points) != 1 || points[0][1] != 59 {
		t.Fatalf("unexpected points: %v", points)
	}
}

func TestEntryNameKeyedByUID(t *testing.T) {
	a := agent.BillingSource{Provider: providerCodeBuddy, Token: "token-a", UID: "uid-1"}
	b := agent.BillingSource{Provider: providerCodeBuddy, Token: "token-b", UID: "uid-1"}
	c := agent.BillingSource{Provider: providerCodeBuddy, Token: "token-a", UID: "uid-2"}

	if entryName(a) != entryName(b) {
		t.Error("rotating the token must not orphan the cache of the same account")
	}
	if entryName(a) == entryName(c) {
		t.Error("different accounts must never share a cache file")
	}
}
