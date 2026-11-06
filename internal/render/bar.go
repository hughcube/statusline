package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/hughcube/statusline/internal/agent"
)

// Git carries the repository segment data; layout is the renderer's job.
type Git struct {
	Branch   string
	Dirty    int
	Ahead    int
	Behind   int
	Add      int
	Del      int
	Worktree string
}

// Billing is the preformatted balance segment. Label names the provider (for
// example "DeepSeek") and is dropped at narrow widths; Stale adds a "?" so a
// cached value that could not be refreshed does not look current.
type Billing struct {
	Label string
	Text  string
	Stale bool
}

// Style captures one CLI's container constraints.
type Style struct {
	// Container is the usable width (WT).
	Container int
	// ByteLimit enables the byte budget when non-zero (CodeBuddy wraps by bytes).
	ByteLimit int
	// Cursor selects CSI cursor positioning (CodeBuddy) instead of ESC0 + spaces.
	Cursor bool
}

// StyleFor returns the container constraints for a client.
func StyleFor(client string, probeW int) Style {
	switch client {
	case "codebuddy":
		return Style{Container: probeW - 1, ByteLimit: probeW, Cursor: true}
	case "antigravity":
		return Style{Container: probeW}
	default:
		return Style{Container: probeW - 4}
	}
}

// Build renders the two status lines without a billing segment.
func Build(f *agent.Fields, client string, probeW int, git Git) string {
	return BuildWith(f, client, probeW, git, Billing{})
}

// BuildWith renders the two status lines including the billing segment. The
// result contains the inner newline but no trailing newline.
func BuildWith(f *agent.Fields, client string, probeW int, git Git, bill Billing) string {
	f = Normalize(f, client)
	st := StyleFor(client, probeW)
	wt := st.Container
	if wt < 20 {
		wt = 20
	}

	l1r := contextSeg(f) + " | " + perfSeg(f)
	rw := SegWidth(l1r)

	gitSeg := gitSegment(git, f.Worktree)
	gw := SegWidth(gitSeg)

	dirmax := wt - rw - gw - 1
	if dirmax < 3 {
		dirmax = 3
	}

	disp := DisplayPath(f.Dir, HomeDir())
	var l1l string
	if st.ByteLimit > 0 {
		bdirmax := st.ByteLimit - ByteWidth(l1r) - ByteWidth(gitSeg) - 1
		if bdirmax < 3 {
			bdirmax = 3
		}
		if bdirmax < dirmax {
			dirmax = bdirmax
		}
		l1l = TruncTailDual(disp, dirmax, bdirmax)
	} else {
		l1l = TruncTail(disp, dirmax)
	}
	l1l += gitSeg

	lw := SegWidth(l1l)
	if sn := sessionLabel(f); sn != "" {
		budget := wt - lw - rw - 3
		if budget >= 6 {
			if st.ByteLimit > 0 {
				bsn := st.ByteLimit - ByteWidth(l1l) - rw - 3
				if bsn >= 6 {
					l1r += " | " + TruncHeadDual(sn, budget, bsn)
					rw = SegWidth(l1r)
				}
			} else {
				l1r += " | " + TruncHead(sn, budget)
				rw = SegWidth(l1r)
			}
		}
	}
	gap := wt - lw - rw
	if gap < 1 {
		gap = 1
	}

	l2 := buildLine2(f, wt, bill)
	pad := wt - SegWidth(l2)
	if pad < 0 {
		pad = 0
	}

	if st.Cursor {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[1G%s\x1b[%dC%s\n", l1l, gap, l1r)
		if pad >= 1 {
			fmt.Fprintf(&b, "\x1b[1G\x1b[%dC%s", pad, l2)
		} else {
			b.WriteString("\x1b[1G" + l2)
		}
		return b.String()
	}
	return l1l + strings.Repeat(" ", gap) + l1r + "\n\x1b[0m" + strings.Repeat(" ", pad) + l2
}

// contextSeg renders used tokens and the window percentage.
func contextSeg(f *agent.Fields) string {
	ctx := f.CtxSize
	if ctx <= 0 {
		ctx = 1048576
	}
	var used int64
	var pct int
	if f.UsedPct != nil && *f.UsedPct >= 0 {
		// CodeBuddy 自带窗口占用:先量化到两位小数再反推 token,保证两个值自洽
		hundredths := int(*f.UsedPct*100 + 0.5)
		used = int64((int64(hundredths)*ctx + 5000) / 10000)
		pct = (hundredths + 50) / 100
	} else {
		used = f.InTokens + f.OutTokens
		pct = int((used*100 + ctx/2) / ctx)
	}
	return fmt.Sprintf("%s(%d%%)", FmtTokens(used), pct)
}

// perfSeg renders In/Out[/Cache][/$cost][/duration].
func perfSeg(f *agent.Fields) string {
	parts := []string{"In:" + FmtTokens(f.InTokens) + " Out:" + FmtTokens(f.OutTokens)}
	if f.CacheRead > 0 {
		parts[0] += " Cache:" + FmtTokens(f.CacheRead)
	}
	if f.CacheCreate > 0 {
		parts[0] += " CacheWrite:" + FmtTokens(f.CacheCreate)
	}
	if f.CostUSD > 0 {
		// Format first, then drop the value: a sub-cent cost must not render as
		// "$0.00" (the legacy implementation did the same check after formatting).
		if cn := fmt.Sprintf("%.2f", f.CostUSD); cn != "0.00" {
			parts = append(parts, "$"+cn)
		}
	}
	if f.DurMS > 0 {
		parts = append(parts, FmtDuration(f.DurMS))
	}
	return strings.Join(parts, " ")
}

// gitSegment renders " branch *3/^2/v1/+9/-4 wt:name".
func gitSegment(g Git, worktree string) string {
	var seg string
	if g.Branch != "" {
		seg = " " + g.Branch
		var items []string
		if g.Dirty > 0 {
			items = append(items, fmt.Sprintf("*%d", g.Dirty))
		}
		switch {
		case g.Ahead > 0 && g.Behind > 0:
			items = append(items, fmt.Sprintf("^%dv%d", g.Ahead, g.Behind))
		case g.Ahead > 0:
			items = append(items, fmt.Sprintf("^%d", g.Ahead))
		case g.Behind > 0:
			items = append(items, fmt.Sprintf("v%d", g.Behind))
		}
		if line := lineSeg(g); line != "" {
			items = append(items, line)
		}
		if len(items) > 0 {
			seg += " " + strings.Join(items, "/")
		}
	} else if line := lineSeg(g); line != "" {
		seg = " " + line
	}
	if worktree != "" {
		seg += " wt:" + worktree
	}
	return seg
}

func lineSeg(g Git) string {
	switch {
	case g.Add != 0 && g.Del != 0:
		return fmt.Sprintf("+%d/-%d", g.Add, g.Del)
	case g.Add != 0:
		return fmt.Sprintf("+%d", g.Add)
	case g.Del != 0:
		return fmt.Sprintf("-%d", g.Del)
	}
	return ""
}

// sessionLabel returns the session name appended to line 1, if any.
func sessionLabel(f *agent.Fields) string {
	if s := strings.TrimSpace(f.Session); s != "" {
		return s
	}
	return BaseName(f.Dir)
}

// buildLine2 degrades progressively: level 0 is the richest, level 4 is model only.
func buildLine2(f *agent.Fields, wt int, bill Billing) string {
	for lvl := 0; lvl <= 4; lvl++ {
		line := line2At(f, lvl, wt, bill)
		if SegWidth(line) <= wt {
			return line
		}
	}
	return line2At(f, 4, wt, bill)
}

func line2At(f *agent.Fields, lvl, wt int, bill Billing) string {
	var parts []string

	acct := Account(f)
	plan := PlanName(f.PlanTier)
	switch {
	case acct != "" && lvl <= 3:
		if plan != "" && lvl <= 2 {
			parts = append(parts, acct+"("+plan+")")
		} else {
			parts = append(parts, acct)
		}
	case plan != "" && lvl <= 2:
		parts = append(parts, plan)
	}

	if b := billingSeg(bill, lvl); b != "" {
		parts = append(parts, b)
	}

	if q := quotaSeg(f.Quotas, lvl); q != "" {
		parts = append(parts, q)
	}

	// MCP follows the same degradation tier as the version: it is useful
	// context, never the last thing to survive a narrow line.
	if f.MCPEnabled > 0 && lvl <= 2 {
		parts = append(parts, fmt.Sprintf("MCP×%d", f.MCPEnabled))
	}

	model := ModelSeg(f)
	if lvl >= 4 {
		limit := wt / 2
		if limit < 20 {
			limit = 20
		}
		model = TruncHead(model, limit)
	}
	if model != "" {
		parts = append(parts, model)
	}
	if lvl <= 2 && f.Version != "" {
		parts = append(parts, "v"+f.Version)
	}
	return strings.Join(parts, " | ")
}

// billingSeg renders the balance. The provider label is the first thing to go
// when width is tight; the amount survives levels 0-3 and disappears with the
// rest of the account context at level 4.
func billingSeg(b Billing, lvl int) string {
	if b.Text == "" || lvl > 3 {
		return ""
	}
	seg := b.Text
	if b.Stale {
		seg += "?"
	}
	if lvl <= 1 && b.Label != "" {
		seg = b.Label + " " + seg
	}
	return seg
}

func quotaSeg(quotas []agent.Quota, lvl int) string {
	var segs []string
	for _, q := range quotas {
		p5, p7 := "", ""
		if q.Used5 >= 0 {
			p5 = fmt.Sprintf("%d%%", q.Used5)
			if lvl <= 1 && !q.Reset5.IsZero() {
				p5 += "(" + q.Reset5.Format("15:04") + ")"
			}
		}
		if q.Used7 >= 0 && lvl <= 3 {
			p7 = fmt.Sprintf("%d%%", q.Used7)
			if lvl == 0 && !q.Reset7.IsZero() {
				p7 += "(" + q.Reset7.Format("02 15:04") + ")"
			}
		}
		seg := p5
		if p5 != "" && p7 != "" {
			seg = p5 + "/" + p7
		} else if p7 != "" {
			seg = p7
		}
		if seg != "" && q.Label != "" {
			seg = q.Label + "·" + seg
		}
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	return strings.Join(segs, " | ")
}

// FmtTokens formats token counts as 1.2K / 3.4M.
func FmtTokens(n int64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%d.%dM", n/1000000, (n%1000000)/100000)
	}
	return fmt.Sprintf("%d.%dK", n/1000, (n%1000)/100)
}

// FmtDuration formats milliseconds as 2m3s / 1h4m.
func FmtDuration(ms int64) string {
	ds := ms / 1000
	h, m, s := ds/3600, (ds%3600)/60, ds%60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}

var _ = time.Now
