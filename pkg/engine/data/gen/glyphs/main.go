// Command glyphs regenerates pkg/engine/data/glyphs_unicode.go from the
// Unicode Consortium's confusables.txt.
//
// The table is not a copy of confusables.txt: it is an inversion of a filtered
// subset, and the filter encodes judgement that PROVENANCE.md explains and this
// program is the executable statement of. Keeping the two in step is the whole
// reason this exists — a filter that lives only in prose has to be re-derived,
// and a re-derivation that lands on a slightly different block set silently
// changes which look-alike domains yatt generates.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const sourceURL = "https://www.unicode.org/Public/security/latest/confusables.txt"

// block is a Unicode block accepted as a plausible homograph source.
type block struct {
	name   string
	lo, hi rune
}

// acceptedBlocks is the filter's load-bearing judgement call.
//
// These are the blocks whose characters are both visually confusable with ASCII
// and plausibly registrable in a domain name. Blocks left out — Mathematical
// Alphanumeric Symbols, Enclosed Alphanumerics, Roman Numerals, Canadian
// Syllabics and similar — contain technically-confusable codepoints that are
// either not IDNA-registrable or not plausible phishing material, so including
// them would inflate the candidate set without adding risk coverage.
var acceptedBlocks = []block{
	{"Latin-1 Supplement", 0x0080, 0x00FF},
	{"Latin Extended-A", 0x0100, 0x017F},
	{"Latin Extended-B", 0x0180, 0x024F},
	{"IPA Extensions", 0x0250, 0x02AF},
	{"Greek and Coptic", 0x0370, 0x03FF},
	{"Cyrillic", 0x0400, 0x04FF},
	{"Cyrillic Supplement", 0x0500, 0x052F},
	{"Armenian", 0x0530, 0x058F},
	{"Halfwidth and Fullwidth Forms", 0xFF00, 0xFFEF},
}

func accepted(r rune) bool {
	for _, b := range acceptedBlocks {
		if r >= b.lo && r <= b.hi {
			return true
		}
	}
	return false
}

var (
	versionRE = regexp.MustCompile(`^#\s*Version:\s*(.+?)\s*$`)
	dateRE    = regexp.MustCompile(`^#\s*Date:\s*(.+?)\s*$`)
)

func main() {
	out := flag.String("o", "glyphs_unicode.go", "output file")
	from := flag.String("from", "", "read confusables.txt from this path instead of fetching it")
	flag.Parse()

	// The work happens in run so the deferred close of the response body
	// actually fires: log.Fatal exits the process and skips every defer.
	if err := run(*out, *from); err != nil {
		log.Fatal(err)
	}
}

func run(out, from string) error {
	body, err := open(from)
	if err != nil {
		return fmt.Errorf("read confusables.txt: %w", err)
	}
	defer func() { _ = body.Close() }()

	table, version, date, err := parse(body)
	if err != nil {
		return fmt.Errorf("parse confusables.txt: %w", err)
	}
	if len(table) == 0 {
		return errors.New("parse confusables.txt: no entries matched the filter, refusing to write an empty table")
	}

	if err := os.WriteFile(out, render(table, version, date), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d targets from confusables.txt %s\n", out, len(table), version)
	return nil
}

func open(path string) (io.ReadCloser, error) {
	if path != "" {
		return os.Open(path)
	}
	resp, err := http.Get(sourceURL) //nolint:gosec // a constant, documented URL
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", sourceURL, resp.Status)
	}
	return resp.Body, nil
}

// parse reads confusables.txt records and returns target -> confusable sources.
//
// Each record is "source ; target ; type # comment". A record is kept when the
// target is a single ASCII letter or digit — the character an attacker wants to
// imitate — and the source is a single codepoint in an accepted block.
func parse(r io.Reader) (map[string][]string, string, string, error) {
	table := make(map[string][]string)
	seen := make(map[string]map[string]bool)
	var version, date string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "#") {
			if m := versionRE.FindStringSubmatch(line); m != nil && version == "" {
				version = m[1]
			}
			if m := dateRE.FindStringSubmatch(line); m != nil && date == "" {
				date = m[1]
			}
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Split(line, ";")
		if len(fields) < 2 {
			continue
		}

		source, ok := singleRune(fields[0])
		if !ok || !accepted(source) {
			continue
		}
		target, ok := singleRune(fields[1])
		if !ok || !isASCIIAlnum(target) {
			continue
		}

		key, value := string(target), string(source)
		if seen[key] == nil {
			seen[key] = make(map[string]bool)
		}
		if seen[key][value] {
			continue
		}
		seen[key][value] = true
		table[key] = append(table[key], value)
	}
	if err := scanner.Err(); err != nil {
		return nil, "", "", err
	}

	// Sort by codepoint so the generated file is byte-identical across runs;
	// the techniques' output ordering is a documented guarantee.
	for k := range table {
		sort.Slice(table[k], func(i, j int) bool { return table[k][i] < table[k][j] })
	}
	return table, version, date, nil
}

// singleRune parses a whitespace-padded hex codepoint field, rejecting the
// multi-codepoint sequences confusables.txt also carries.
func singleRune(field string) (rune, bool) {
	parts := strings.Fields(strings.TrimSpace(field))
	if len(parts) != 1 {
		return 0, false
	}
	cp, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil || cp > unicode.MaxRune {
		return 0, false
	}
	return rune(cp), true
}

func isASCIIAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func render(table map[string][]string, version, date string) []byte {
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated from Unicode confusables.txt; DO NOT EDIT BY HAND.\n")
	fmt.Fprintf(&b, "// Source:  %s\n", sourceURL)
	fmt.Fprintf(&b, "// Version: %s (dated %s)\n", version, date)
	fmt.Fprintf(&b, "// Regenerate with: go generate ./pkg/engine/data\n")
	fmt.Fprintf(&b, "// See PROVENANCE.md for the filter this applies and the reasoning behind it.\n\n")
	b.WriteString("package data\n\n")
	b.WriteString("// GlyphsUnicode maps an ASCII letter or digit to the Unicode characters\n")
	b.WriteString("// visually confusable with it, restricted to scripts plausibly used in\n")
	b.WriteString("// homograph attacks (Latin-1 Supplement, Latin Extended-A/B, IPA Extensions,\n")
	b.WriteString("// Greek and Coptic, Cyrillic (+ Supplement), Armenian, and the Halfwidth and\n")
	b.WriteString("// Fullwidth Forms block). See PROVENANCE.md.\n")
	b.WriteString("var GlyphsUnicode = map[string][]string{\n")
	for _, k := range keys {
		quoted := make([]string, 0, len(table[k]))
		for _, v := range table[k] {
			quoted = append(quoted, strconv.Quote(v))
		}
		fmt.Fprintf(&b, "\t%s: {%s},\n", strconv.Quote(k), strings.Join(quoted, ", "))
	}
	b.WriteString("}\n")
	return []byte(b.String())
}
