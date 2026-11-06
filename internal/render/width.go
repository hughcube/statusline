// Package render provides terminal width measurement and truncation.
//
// The semantics are deliberately identical to the legacy shell version:
//   - display width: ASCII is 1 column, other runes are 2, decorative glyphs
//     (see Glyphs) count as 1;
//   - byte width: ASCII is 1 byte, everything else counts as 3 (historical
//     behaviour, including 4-byte emoji);
//   - truncation: budget is max-1 and an ellipsis is appended.
package render

import "strings"

// Glyphs are decorative single-column symbols: they count as 1 column,
// unlike ordinary non-ASCII runes which count as 2.
var Glyphs = map[rune]struct{}{
	'█': {}, '░': {}, '▲': {}, '▼': {}, '↑': {}, '↓': {},
	'↻': {}, '…': {}, '·': {}, '⚡': {}, '💾': {}, '🔒': {},
	'📨': {}, '⬤': {},
}

// SegWidth returns the number of terminal columns a string occupies.
func SegWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x20 && r <= 0x7e:
			w++
		default:
			if _, ok := Glyphs[r]; ok {
				w++
			} else {
				w += 2
			}
		}
	}
	return w
}

// ByteWidth returns the historical byte width: ASCII 1, everything else 3.
// Hosts wrap by bytes while terminals measure columns, so both must hold.
func ByteWidth(s string) int {
	ascii, total := 0, 0
	for _, r := range s {
		total++
		if r >= 0x20 && r <= 0x7e {
			ascii++
		}
	}
	return ascii + 3*(total-ascii)
}

// runeWidth is the per-rune width used while truncating.
func runeWidth(r rune) int {
	if (r >= 0x20 && r <= 0x7e) || r == '…' {
		return 1
	}
	return 2
}

// TruncHead keeps the head and appends an ellipsis when too wide.
func TruncHead(s string, max int) string {
	if SegWidth(s) <= max {
		return s
	}
	budget := max - 1
	if budget < 1 {
		budget = 1
	}
	acc := 0
	var b strings.Builder
	for _, r := range s {
		w := runeWidth(r)
		if acc+w > budget {
			break
		}
		acc += w
		b.WriteRune(r)
	}
	b.WriteString("…")
	return b.String()
}

// TruncTail keeps the tail and prefixes an ellipsis when too wide.
func TruncTail(s string, max int) string {
	if SegWidth(s) <= max {
		return s
	}
	budget := max - 1
	if budget < 1 {
		budget = 1
	}
	runes := []rune(s)
	acc := 0
	cut := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		w := runeWidth(runes[i])
		if acc+w > budget {
			break
		}
		acc += w
		cut = i
	}
	return "…" + string(runes[cut:])
}

// TruncHeadDual is bounded by both columns and bytes, whichever is tighter.
func TruncHeadDual(s string, dmax, bmax int) string {
	if SegWidth(s) <= dmax && ByteWidth(s) <= bmax {
		return s
	}
	db, bb := dmax-1, bmax-3
	if db < 1 {
		db = 1
	}
	if bb < 1 {
		bb = 1
	}
	acc, bacc := 0, 0
	var b strings.Builder
	for _, r := range s {
		var w, n int
		switch {
		case r >= 0x20 && r <= 0x7e:
			w, n = 1, 1
		case r == '…':
			w, n = 1, 3
		default:
			w, n = 2, 3
		}
		if acc+w > db || bacc+n > bb {
			break
		}
		acc += w
		bacc += n
		b.WriteRune(r)
	}
	b.WriteString("…")
	return b.String()
}

// TruncTailDual mirrors TruncHeadDual but keeps the tail.
func TruncTailDual(s string, dmax, bmax int) string {
	if SegWidth(s) <= dmax && ByteWidth(s) <= bmax {
		return s
	}
	db, bb := dmax-1, bmax-3
	if db < 1 {
		db = 1
	}
	if bb < 1 {
		bb = 1
	}
	runes := []rune(s)
	acc, bacc := 0, 0
	cut := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		var w, n int
		switch r := runes[i]; {
		case r >= 0x20 && r <= 0x7e:
			w, n = 1, 1
		case r == '…':
			w, n = 1, 3
		default:
			w, n = 2, 3
		}
		if acc+w > db || bacc+n > bb {
			break
		}
		acc += w
		bacc += n
		cut = i
	}
	return "…" + string(runes[cut:])
}
