package render

import "testing"

func TestCreditsSegmentDegrades(t *testing.T) {
	b := Billing{Account: "懂", Text: "3,359", Expiring: "59"}
	cases := []struct {
		lvl  int
		want string
	}{
		{0, "懂 (3,359 | 59 in 24h)"},
		{1, "懂 (3,359 | 59 in 24h)"},
		{2, "懂 (3,359)"},
		{3, "3,359"},
		{4, ""},
	}
	for _, c := range cases {
		if got := billingSeg(b, c.lvl); got != c.want {
			t.Errorf("lvl %d = %q, want %q", c.lvl, got, c.want)
		}
	}
}

func TestCreditsSegmentStaleMarker(t *testing.T) {
	b := Billing{Account: "懂", Text: "3,359", Expiring: "59", Stale: true}
	if got := billingSeg(b, 0); got != "懂 (3,359? | 59 in 24h)" {
		t.Errorf("stale marker missing: %q", got)
	}
}

func TestCreditsSegmentWithoutExpiring(t *testing.T) {
	b := Billing{Account: "懂", Text: "3,359"}
	if got := billingSeg(b, 0); got != "懂 (3,359)" {
		t.Errorf("= %q, want 懂 (3,359)", got)
	}
}
