package data

// Merge combines glyph tables into one, concatenating the substitution
// lists for a window present in more than one table.
//
// Order is deterministic despite Go's randomized map iteration: each key's
// merged slice is built by appending tables[0][key], then tables[1][key],
// and so on, in argument order — independent of which key Merge happens to
// visit first. Passing GlyphsASCII before GlyphsUnicode means ASCII
// substitutions are tried before Unicode ones wherever both exist for the
// same window.
func Merge(tables ...map[string][]string) map[string][]string {
	out := make(map[string][]string)
	for _, table := range tables {
		for key, values := range table {
			out[key] = append(append([]string(nil), out[key]...), values...)
		}
	}
	return out
}
