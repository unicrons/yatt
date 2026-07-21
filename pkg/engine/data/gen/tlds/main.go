// Command tlds regenerates pkg/engine/data/tlds_iana.go from the IANA Root
// Zone Database's list of delegated top-level domains.
//
// The transformation is only "lowercase and format", so this program exists
// less to encode judgement than to stamp the source's own version line into
// the generated file: an undated TLD snapshot gives no way to tell a current
// list from a three-year-old one.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

const sourceURL = "https://data.iana.org/TLD/tlds-alpha-by-domain.txt"

// perLine is how many TLDs to emit per source line. The list is ~1400 entries;
// one per line would make the file unreadable and its diffs unreviewable.
const perLine = 8

func main() {
	out := flag.String("o", "tlds_iana.go", "output file")
	from := flag.String("from", "", "read the TLD list from this path instead of fetching it")
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
		return fmt.Errorf("read TLD list: %w", err)
	}
	defer func() { _ = body.Close() }()

	tlds, version, err := parse(body)
	if err != nil {
		return fmt.Errorf("parse TLD list: %w", err)
	}
	// The root zone has had well over a thousand TLDs since the 2013 gTLD
	// expansion; a handful means we parsed something that was not the list.
	if len(tlds) < 100 {
		return fmt.Errorf("parse TLD list: got only %d TLDs, refusing to write a truncated list", len(tlds))
	}

	if err := os.WriteFile(out, render(tlds, version), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d TLDs from IANA %s\n", out, len(tlds), version)
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

// parse reads the TLD list, returning the lowercased TLDs and the source's own
// version line ("Version 2026062302, Last Updated ...").
func parse(r io.Reader) ([]string, string, error) {
	var tlds []string
	var version string

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if version == "" {
				version = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			}
			continue
		}
		tlds = append(tlds, strings.ToLower(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	return tlds, version, nil
}

func render(tlds []string, version string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated from the IANA Root Zone Database TLD list; DO NOT EDIT BY HAND.\n")
	fmt.Fprintf(&b, "// Source:  %s\n", sourceURL)
	fmt.Fprintf(&b, "// %s\n", version)
	fmt.Fprintf(&b, "// Regenerate with: go generate ./pkg/engine/data\n")
	fmt.Fprintf(&b, "// See PROVENANCE.md.\n\n")
	b.WriteString("package data\n\n")
	b.WriteString("// IANATLDs lists every top-level domain IANA's Root Zone Database publishes,\n")
	b.WriteString("// lowercased, including the punycode (\"xn--\") form of internationalized\n")
	b.WriteString("// ccTLDs. It is the \"full\" TLD profile's candidate list. See PROVENANCE.md.\n")
	b.WriteString("var IANATLDs = []string{\n")
	for i := 0; i < len(tlds); i += perLine {
		end := min(i+perLine, len(tlds))
		quoted := make([]string, 0, end-i)
		for _, tld := range tlds[i:end] {
			quoted = append(quoted, fmt.Sprintf("%q", tld))
		}
		fmt.Fprintf(&b, "\t%s,\n", strings.Join(quoted, ", "))
	}
	b.WriteString("}\n")
	return []byte(b.String())
}
