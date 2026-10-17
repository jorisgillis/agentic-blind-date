// Command crap computes the CRAP score (Change Risk Analysis and
// Predictions) for every function in the root package, given a Go
// coverage profile (ADR-0023).
//
// Formula (Savoia/Evans, as implemented by PHPUnit's php-code-coverage):
//
//	CRAP = complexity^2 * (1 - coverage)^3 + complexity
//
// Coverage is a fraction in [0,1]; coverage >= 95% counts as fully
// covered (the PHPUnit clamp). Cyclomatic complexity counts the
// standard decision points (if/for/range/case/select-comm/&&/||) with a
// base of 1, matching gocyclo: default clauses are free. Function
// literals are attributed to the enclosing named function, so closures
// cannot hide complexity from the gate. main() is excluded, the same
// exclusion ADR-0008 gives it in the coverage gate.
//
// Usage:
//
//	go test -coverprofile=coverage.out .
//	go run ./scripts/crap [-profile coverage.out]
//	go run ./scripts/crap -failing   # print only the failing count
//	go run ./scripts/crap -gate      # exit 1 when any gated function fails
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// fullyCoveredAt is the coverage fraction at and above which a function
// counts as fully covered (PHPUnit's clamp).
const fullyCoveredAt = 0.95

type fn struct {
	file       string
	line       int
	end        int
	name       string
	complexity int
	stmts      int
	covered    int
}

// score is a function with its coverage fraction and CRAP score.
type score struct {
	fn   fn
	cov  float64
	crap float64
}

func main() {
	profile := flag.String("profile", "coverage.out", "coverage profile (go test -coverprofile)")
	threshold := flag.Float64("threshold", 6.0, "CRAP threshold; gated functions with CRAP >= threshold fail")
	failing := flag.Bool("failing", false, "print only the count of functions failing the gate (machine-readable)")
	gate := flag.Bool("gate", false, "exit non-zero when any gated function fails")
	flag.Parse()

	fns, err := scanModule(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		os.Exit(1)
	}
	blocks, err := readProfile(*profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile:", err)
		os.Exit(1)
	}
	attach(blocks, fns)

	all, failingFns := scoreFunctions(fns, *threshold)

	if *failing {
		fmt.Println(len(failingFns))
	}
	if !*failing {
		printReport(all, failingFns, *threshold)
	}
	if *gate && len(failingFns) > 0 {
		os.Exit(1)
	}
}

func printReport(all, failingFns []score, threshold float64) {
	fmt.Printf("%-45s %4s %8s %8s\n", "FUNCTION", "CC", "COV", "CRAP")
	fmt.Println(strings.Repeat("-", 72))
	for _, s := range all {
		mark := ""
		if isFailing(s, threshold) {
			mark = "  FAIL"
		}
		fmt.Printf("%-45s %4d %7.1f%% %8.2f%s\n",
			displayName(s.fn), s.fn.complexity, s.cov*100, s.crap, mark)
	}
	fmt.Printf("\n%d functions scored, %d fail the CRAP < %.0f gate (main excluded).\n",
		len(all), len(failingFns), threshold)
}

// scoreFunctions scores every function and returns the full list plus
// the gated failures (main excluded), each worst-first.
func scoreFunctions(fns []fn, threshold float64) (all, failingFns []score) {
	for _, f := range fns {
		s := score{
			fn:   f,
			cov:  coverageOf(f),
			crap: crapScore(f.complexity, coverageOf(f)),
		}
		all = append(all, s)
		if isFailing(s, threshold) {
			failingFns = append(failingFns, s)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].crap > all[j].crap })
	sort.SliceStable(failingFns, func(i, j int) bool { return failingFns[i].crap > failingFns[j].crap })
	return all, failingFns
}

// isFailing reports whether a scored function fails the gate: CRAP at
// or above the threshold, with main() excluded (ADR-0008).
func isFailing(s score, threshold float64) bool {
	return s.fn.name != "main" && s.crap >= threshold
}

// coverageOf returns the fraction of the function's statements covered
// by the profile; a function with no profile blocks is uncovered.
func coverageOf(f fn) float64 {
	if f.stmts == 0 {
		return 0
	}
	return float64(f.covered) / float64(f.stmts)
}

// crapScore applies the CRAP formula, treating coverage >= 95% as
// fully covered.
func crapScore(complexity int, cov float64) float64 {
	if cov >= fullyCoveredAt {
		cov = 1
	}
	c := float64(complexity)
	return c*c*(1-cov)*(1-cov)*(1-cov) + c
}

func displayName(f fn) string {
	base := filepath.Base(f.file)
	if f.name == "main" {
		return base + ": main()"
	}
	return fmt.Sprintf("%s: %s (L%d)", base, f.name, f.line)
}

// scanModule parses every non-test .go file under root and records each
// named function. Tooling, templates, and docs are out of gate scope.
func scanModule(root string) ([]fn, error) {
	var fns []fn
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "e2e", "templates", "docs", "scripts":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				if t := fd.Recv.List[0].Type; t != nil {
					name = fmt.Sprintf("%s.%s", exprString(t), fd.Name.Name)
				}
			}
			fns = append(fns, fn{
				file:       path,
				line:       fset.Position(fd.Pos()).Line,
				end:        fset.Position(fd.End()).Line,
				name:       name,
				complexity: complexityOf(fd),
			})
		}
		return nil
	})
	return fns, err
}

func exprString(x ast.Expr) string {
	switch t := x.(type) {
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return exprString(t.X)
	default:
		return "?"
	}
}

// complexityOf returns the cyclomatic complexity of the function,
// including nested function literals, following gocyclo: base 1, +1 per
// decision point, default clauses free.
func complexityOf(fd *ast.FuncDecl) int {
	v := &complexityVisitor{n: 1}
	ast.Walk(v, fd.Body)
	return v.n
}

type complexityVisitor struct{ n int }

func (v *complexityVisitor) Visit(n ast.Node) ast.Visitor {
	switch x := n.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
		v.n++
	case *ast.CaseClause:
		if x.List != nil {
			v.n++
		}
	case *ast.CommClause:
		if x.Comm != nil {
			v.n++
		}
	case *ast.BinaryExpr:
		if x.Op == token.LAND || x.Op == token.LOR {
			v.n++
		}
	}
	return v
}

type block struct {
	file               string
	startLine, endLine int
	stmts              int
	count              int
}

func readProfile(path string) ([]block, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }() // the scanner drained the file; the close cannot matter
	var blocks []block
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			continue // "mode:" line
		}
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		loc := parts[0]
		stmts, _ := strconv.Atoi(parts[1])
		count, _ := strconv.Atoi(parts[2])
		colon := strings.LastIndex(loc, ":")
		if colon < 0 {
			continue
		}
		file := strings.TrimSuffix(loc[:colon], ".go")
		file = file[strings.LastIndex(file, "/")+1:] // basename without .go
		pos := strings.SplitN(loc[colon+1:], ",", 2)
		if len(pos) != 2 {
			continue
		}
		start, _, _ := strings.Cut(pos[0], ".")
		end, _, _ := strings.Cut(pos[1], ".")
		s, _ := strconv.Atoi(start)
		e, _ := strconv.Atoi(end)
		blocks = append(blocks, block{file, s, e, stmts, count})
	}
	return blocks, sc.Err()
}

// attach sums profile statements into the function whose line range
// contains each block. File matching is by basename-without-suffix,
// robust to the module-qualified paths that go test -coverprofile emits.
func attach(blocks []block, fns []fn) {
	byFile := map[string][]*fn{}
	for i := range fns {
		base := strings.TrimSuffix(filepath.Base(fns[i].file), ".go")
		byFile[base] = append(byFile[base], &fns[i])
	}
	for _, b := range blocks {
		for _, f := range byFile[b.file] {
			// Line 0 means the whole file; ignore those markers.
			if f.line <= 0 {
				continue
			}
			if b.startLine >= f.line && (f.end == 0 || b.startLine <= f.end) {
				f.stmts += b.stmts
				if b.count > 0 {
					f.covered += b.stmts
				}
				break
			}
		}
	}
}
