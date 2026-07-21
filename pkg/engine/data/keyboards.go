// Package data holds yatt's generated permutation tables — keyboard
// adjacency, homoglyph substitutions, and TLD lists — so pkg/engine's
// technique code stays free of large literals. See PROVENANCE.md for the
// source behind every table in this package.
package data

import (
	"math"
	"sort"
	"strings"
)

// keyboardRow is one physical row of a keyboard layout: its keys, left to
// right, and the row's horizontal offset in key-widths from the row above —
// the stagger that makes "adjacent key" a geometric fact rather than a
// same-index guess.
type keyboardRow struct {
	keys   string
	offset float64
}

// keyboardLayouts lists each layout's physical rows: the three keyboards
// dnstwist also covers (qwerty, qwertz, azerty), restricted to the letters
// and digits every one of them shares a position for. Each row is offset a
// further half key-width from the one above, which is the standard ANSI/ISO
// stagger and is what makes each key sit visually between the two keys of
// the row above and below it. The adjacency table is computed from the
// physical layouts themselves; see PROVENANCE.md.
var keyboardLayouts = map[string][]keyboardRow{
	"qwerty": {
		{keys: "1234567890", offset: 0},
		{keys: "qwertyuiop", offset: 0.5},
		{keys: "asdfghjkl", offset: 1.0},
		{keys: "zxcvbnm", offset: 1.5},
	},
	"qwertz": {
		{keys: "1234567890", offset: 0},
		{keys: "qwertzuiop", offset: 0.5},
		{keys: "asdfghjkl", offset: 1.0},
		{keys: "yxcvbnm", offset: 1.5},
	},
	"azerty": {
		{keys: "1234567890", offset: 0},
		{keys: "azertyuiop", offset: 0.5},
		{keys: "qsdfghjklm", offset: 1.0},
		{keys: "wxcvbn", offset: 1.5},
	},
}

// KeyboardLayoutNames lists the layouts in Keyboards, in a fixed order —
// map iteration order is not deterministic, and technique output order must
// be, so any caller iterating every layout uses this slice rather than
// ranging over Keyboards directly.
var KeyboardLayoutNames = []string{"qwerty", "qwertz", "azerty"}

// Keyboards maps a layout name to its adjacency table: each key to the
// string of keys physically touching it (same row, or the row above/below
// within the stagger), sorted for determinism.
var Keyboards = buildKeyboards()

func buildKeyboards() map[string]map[rune]string {
	out := make(map[string]map[rune]string, len(keyboardLayouts))
	for name, rows := range keyboardLayouts {
		out[name] = adjacency(rows)
	}
	return out
}

type keyPosition struct {
	key  rune
	x, y float64
}

// adjacencyRadius is the maximum center-to-center distance, in key-widths,
// for two keys to count as touching. With a half-key stagger per row, the
// two diagonal neighbors in the row above or below sit at
// sqrt(0.5² + 1²) ≈ 1.118 key-widths away; same-row neighbors sit at
// exactly 1. The radius clears both while excluding a same-row key two
// positions over (distance 2) or a key two rows away.
const adjacencyRadius = 1.12

func adjacency(rows []keyboardRow) map[rune]string {
	var positions []keyPosition
	for y, row := range rows {
		for x, k := range row.keys {
			positions = append(positions, keyPosition{key: k, x: float64(x) + row.offset, y: float64(y)})
		}
	}

	out := make(map[rune]string, len(positions))
	for _, p := range positions {
		var neighbors []rune
		for _, q := range positions {
			if q.key == p.key {
				continue
			}
			if math.Hypot(q.x-p.x, q.y-p.y) <= adjacencyRadius {
				neighbors = append(neighbors, q.key)
			}
		}
		sort.Slice(neighbors, func(i, j int) bool { return neighbors[i] < neighbors[j] })
		var b strings.Builder
		for _, n := range neighbors {
			b.WriteRune(n)
		}
		out[p.key] = b.String()
	}
	return out
}
