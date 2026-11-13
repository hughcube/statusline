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
	// Account and Expiring switch the segment to the credits style
	// "懂 (3,359 | 59 in 24h)": one group, dropped from the inside out. They
	// stay empty for balance-style providers such as DeepSeek.
	Account  string
	Expiring string
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
	// Balance and credits come from outside the payload (JWT claims, cached
	// API responses): keep them on one line before they reach the renderer.
	bill.Label, bill.Text = OneLine(bill.Label), OneLine(bill.Text)
	bill.Account, bill.Expiring = OneLine(bill.Account), OneLine(bill.Expiring)
	st := StyleFor(client, probeW)
	wt := st.Container
	if wt < 20 {
		wt = 20
	}

	disp := OneLine(DisplayPath(f.Dir, HomeDir()))
	gitSeg := gitSegment(git, f.Worktree)
	ctx := contextSeg(f)

	// Line 1 is fitted, never assumed to fit. The directory used to be the only
	// segment that shrank, so a long branch name plus the usage counters could
	// push the line past the container; CodeBuddy then wrapped it and the status
	// area gained a row. Each tier gives up a little more usage detail, and the
	// last resort truncates the right side itself, so the invariant holds at any
	// width.
	var l1l, l1r string
	gap := 1
	for _, perf := range perfTiers(f) {
		l1l, l1r, gap = layoutLine1(f, disp, gitSeg, ctx, perf, st, wt)
		if fitsLine(l1l, l1r, gap, st, wt) {
			break
		}
	}
	// Last resort, and the guarantee behind the invariant: the path yields
	// before the numbers do, and below roughly 25 columns the two halves simply
	// cannot share the container — then the numbers are dropped rather than
	// drawn past the edge.
	if !fitsLine(l1l, l1r, gap, st, wt) {
		if SegWidth(l1l) > wt-2 {
			l1l = TruncTail(l1l, wt-2)
		}
		budget := wt - SegWidth(l1l) - 1
		if budget < 1 {
			budget = 1
		}
		if st.ByteLimit > 0 {
			b := st.ByteLimit - ByteWidth(l1l) - 1
			if b < 1 {
				b = 1
			}
			l1r = TruncHeadDual(l1r, budget, b)
		} else {
			l1r = TruncHead(l1r, budget)
		}
		gap = wt - SegWidth(l1l) - SegWidth(l1r)
		if gap < 1 {
			gap = 1
		}
	}
	if !fitsLine(l1l, l1r, gap, st, wt) {
		l1r = ""
		gap = 1
		if SegWidth(l1l) > wt-1 {
			l1l = TruncTail(l1l, wt-1)
		}
	}

	l2 := buildLine2(f, st, wt, bill)
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

// perfSeg renders In/Out[/Cache][/CacheWrite][/$cost][/duration].
func perfSeg(f *agent.Fields) string { return perfSegAt(f, 0) }

// perfSegAt renders the usage segment with an explicit drop level. Higher
// levels give up the tail first: duration and cost are the values a reader can
// also find elsewhere, the cache counters next; the token pair never goes.
func perfSegAt(f *agent.Fields, lvl int) string {
	parts := []string{"In:" + FmtTokens(f.InTokens) + " Out:" + FmtTokens(f.OutTokens)}
	if lvl <= 4 && f.CacheRead > 0 {
		parts[0] += " Cache:" + FmtTokens(f.CacheRead)
	}
	if lvl <= 3 && f.CacheCreate > 0 {
		parts[0] += " CacheWrite:" + FmtTokens(f.CacheCreate)
	}
	if lvl <= 2 && f.CostUSD > 0 {
		// Format first, then drop the value: a sub-cent cost must not render as
		// "$0.00" (the legacy implementation did the same check after formatting).
		if cn := fmt.Sprintf("%.2f", f.CostUSD); cn != "0.00" {
			parts = append(parts, "$"+cn)
		}
	}
	if lvl <= 1 && f.DurMS > 0 {
		parts = append(parts, FmtDuration(f.DurMS))
	}
	return strings.Join(parts, " ")
}

// perfTiers lists the usage segment from richest to plainest, without
// duplicates, so the fit loop never tries the same string twice.
func perfTiers(f *agent.Fields) []string {
	out := make([]string, 0, 6)
	for lvl := 0; lvl <= 5; lvl++ {
		s := perfSegAt(f, lvl)
		if len(out) > 0 && out[len(out)-1] == s {
			continue
		}
		out = append(out, s)
	}
	return out
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

// sessionLabel returns the session name appended to line 1, if any. The name is
// user-authored text (CodeBuddy derives it from the first message) and must be
// flattened before its width is measured.
func sessionLabel(f *agent.Fields) string {
	if s := OneLine(f.Session); s != "" {
		return s
	}
	return OneLine(BaseName(f.Dir))
}

// fitsLine reports whether the line holds in both budgets.
//
// Columns are measured on the whole line: the terminal wraps at the container
// width, so the gap counts. The byte budget measures content only — left plus
// right, not the padding between them — which is the contract this renderer has
// always used for CodeBuddy: a non-ASCII rune costs three bytes where it
// occupies two columns, and that inflation belongs to the text, not to the
// spaces that push the right block flush.
func fitsLine(left, right string, gap int, st Style, wt int) bool {
	if SegWidth(left)+gap+SegWidth(right) > wt {
		return false
	}
	if st.ByteLimit > 0 && ByteWidth(left)+ByteWidth(right) > st.ByteLimit {
		return false
	}
	return true
}

// layoutLine1 assembles the left (directory + git) and right (usage + session)
// halves for one usage tier.
//
// The directory is truncated from the tail and the git segment from the head:
// the branch name is the part worth keeping. The git segment yields before the
// directory does, but only down to three columns, below which it is dropped so
// the path survives. The session name only takes what is genuinely left over,
// which is what stops a long auto-derived title from pushing the line out.
func layoutLine1(f *agent.Fields, disp, gitSeg, ctx, perf string, st Style, wt int) (left, right string, gap int) {
	right = ctx + " | " + perf

	// One column for the gap, three for the directory.
	avail := wt - SegWidth(right) - 4
	bavail := 0
	if st.ByteLimit > 0 {
		bavail = st.ByteLimit - ByteWidth(right) - 4
	}
	gs := gitSeg
	if gs != "" {
		switch {
		case st.ByteLimit > 0 && (SegWidth(gs) > avail || ByteWidth(gs) > bavail):
			if avail < 3 || bavail < 3 {
				gs = ""
			} else {
				gs = TruncHeadDual(gs, avail, bavail)
			}
		case st.ByteLimit == 0 && SegWidth(gs) > avail:
			if avail < 3 {
				gs = ""
			} else {
				gs = TruncHead(gs, avail)
			}
		}
	}

	gw, gb := SegWidth(gs), ByteWidth(gs)
	dirmax := wt - SegWidth(right) - gw - 1
	if dirmax < 3 {
		dirmax = 3
	}
	if st.ByteLimit > 0 {
		bdirmax := st.ByteLimit - ByteWidth(right) - gb - 1
		if bdirmax < 3 {
			bdirmax = 3
		}
		if bdirmax < dirmax {
			dirmax = bdirmax
		}
		left = TruncTailDual(disp, dirmax, bdirmax)
	} else {
		left = TruncTail(disp, dirmax)
	}
	left += gs

	lw := SegWidth(left)
	if sn := sessionLabel(f); sn != "" {
		budget := wt - lw - SegWidth(right) - 3
		if budget >= 6 {
			if st.ByteLimit > 0 {
				bsn := st.ByteLimit - ByteWidth(left) - ByteWidth(right) - 3
				if bsn >= 6 {
					right += " | " + TruncHeadDual(sn, budget, bsn)
				}
			} else {
				right += " | " + TruncHead(sn, budget)
			}
		}
	}

	gap = wt - SegWidth(left) - SegWidth(right)
	if gap < 1 {
		gap = 1
	}
	return left, right, gap
}

// buildLine2 degrades progressively: level 0 is the richest, level 4 is model
// only. Both budgets are enforced at every level, because a CJK account name in
// the credits segment costs three bytes for every two columns.
func buildLine2(f *agent.Fields, st Style, wt int, bill Billing) string {
	for lvl := 0; lvl <= 4; lvl++ {
		line := line2At(f, lvl, wt, bill)
		if SegWidth(line) <= wt && (st.ByteLimit == 0 || ByteWidth(line) <= st.ByteLimit) {
			return line
		}
	}
	line := line2At(f, 4, wt, bill)
	if st.ByteLimit > 0 && ByteWidth(line) > st.ByteLimit {
		line = TruncHeadDual(line, wt, st.ByteLimit)
	}
	return line
}

func line2At(f *agent.Fields, lvl, wt int, bill Billing) string {
	var parts []string

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

	rest := strings.Join(parts, " | ")
	if bill.Account != "" {
		return rest
	}

	avail := wt
	if rest != "" {
		avail = wt - SegWidth(rest) - 3 // 3 columns for " | "
	}
	acctSeg := buildAccountSeg(f, lvl, avail)
	if acctSeg != "" {
		if rest != "" {
			return acctSeg + " | " + rest
		}
		return acctSeg
	}
	return rest
}

// buildAccountSeg renders the account name and plan tier within the available
// width budget, truncating the account first so quotas and reset times survive.
func buildAccountSeg(f *agent.Fields, lvl, avail int) string {
	acct := Account(f)
	plan := PlanName(f.PlanTier)

	if acct == "" && plan == "" {
		return ""
	}

	if acct == "" {
		if lvl <= 2 && SegWidth(plan) <= avail {
			return plan
		}
		return ""
	}

	if lvl >= 4 || avail <= 0 {
		return ""
	}

	if plan != "" && lvl <= 2 {
		planPart := "(" + plan + ")"
		full := acct + planPart
		if SegWidth(full) <= avail {
			return full
		}
		pw := SegWidth(planPart)
		if avail >= pw+2 {
			return TruncHead(acct, avail-pw) + planPart
		}
		if SegWidth(plan) <= avail {
			return plan
		}
		return ""
	}

	if lvl <= 3 {
		if SegWidth(acct) <= avail {
			return acct
		}
		if lvl == 0 {
			return acct
		}
		if avail >= 2 {
			return TruncHead(acct, avail)
		}
	}
	return ""
}

// billingSeg renders the balance. The provider label is the first thing to go
// when width is tight; the amount survives levels 0-3 and disappears with the
// rest of the account context at level 4.
func billingSeg(b Billing, lvl int) string {
	if b.Text == "" || lvl > 3 {
		return ""
	}
	if b.Account != "" {
		total := b.Text
		if b.Stale {
			total += "?"
		}
		switch {
		case lvl <= 1 && b.Expiring != "":
			return b.Account + " (" + total + " | " + b.Expiring + " in 24h)"
		case lvl <= 2:
			return b.Account + " (" + total + ")"
		default:
			return total
		}
	}
	seg := b.Text
	if b.Stale {
		seg += "?"
	}
	if lvl == 0 && b.Label != "" {
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
			if lvl <= 2 && !q.Reset5.IsZero() {
				p5 += "(" + q.Reset5.Format("15:04") + ")"
			}
		}
		if q.Used7 >= 0 && lvl <= 3 {
			p7 = fmt.Sprintf("%d%%", q.Used7)
			if lvl <= 2 && !q.Reset7.IsZero() {
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
