// Command specdiff compares two vendored Logto OpenAPI documents operation by operation and classifies every
// difference as breaking, changed or additive. It is how a new Logto minor is judged before it enters the support
// window (api/logto/README.md).
//
// Usage: go run ./tools/specdiff [-used tools/specdiff/used.txt] <from.json> <to.json>
//
// Every finding is printed. The exit status is 1 only when a BREAKING change touches an operation listed in the
// used file (one operationId per line, `#` comments): the operations the kinds call. An empty or missing used file
// means no operation is used yet, so nothing can fail.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

func main() {
	used := flag.String("used", "tools/specdiff/used.txt", "file listing the operationIds the kinds call")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: specdiff [-used file] <from.json> <to.json>")
		os.Exit(2)
	}
	code, err := run(flag.Arg(0), flag.Arg(1), *used, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "specdiff:", err)
		os.Exit(2)
	}
	os.Exit(code)
}

func run(fromPath, toPath, usedPath string, w io.Writer) (int, error) {
	from, err := load(fromPath)
	if err != nil {
		return 0, err
	}
	to, err := load(toPath)
	if err != nil {
		return 0, err
	}
	used, err := loadUsed(usedPath)
	if err != nil {
		return 0, err
	}
	findings := Diff(from, to)
	return report(findings, used, w), nil
}

// report prints the findings grouped by level and returns the exit status.
func report(findings []Finding, used map[string]bool, w io.Writer) int {
	code := 0
	for _, level := range []Level{Breaking, Changed, Additive} {
		var lines []string
		for _, f := range findings {
			if f.Level != level {
				continue
			}
			mark := ""
			if used[f.OperationID] {
				mark = "  [USED]"
				if level == Breaking {
					code = 1
				}
			}
			lines = append(lines, "  "+f.String()+mark)
		}
		_, _ = fmt.Fprintf(w, "## %s: %d\n", level, len(lines))
		for _, l := range lines {
			_, _ = fmt.Fprintln(w, l)
		}
	}
	if code != 0 {
		_, _ = fmt.Fprintln(w, "specdiff: a breaking change touches an operation the kinds use ([USED] above)")
	}
	return code
}

func load(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

func loadUsed(path string) (map[string]bool, error) {
	out := map[string]bool{}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out, sc.Err()
}
