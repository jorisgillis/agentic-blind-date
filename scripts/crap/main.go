// Command crap computes the CRAP score (Change Risk Anti-Pattern) for every
// function in this module, given a Go coverage profile.
//
// Formula (Savoia/Evans, as implemented by PHPUnit's php-code-coverage):
//
//	CRAP = complexity^2 * (1 - coverage)^3 + complexity
//
// Coverage is a fraction in [0,1]. Cyclomatic complexity counts the standard
// decision points (if/for/range/case/select-comm/&&/||) with a base of 1,
// matching gocyclo. Function literals are attributed to the enclosing
// named function, so closures cannot hide complexity from the gate.
//
// Usage: go run ./scripts/crap -profile coverage.out [-gate 6]
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

type fn struct {
	file       string
	line       int
	end        int
	name       string
	complexity int
	stmts      int
	covered    int
}

func main() {
	profile := flag.String("profile", "coverage.out", "coverage profile (go test -coverprofile)")
	gate := flag.Float64("gate", 6.0, "CRAP threshold; report functions with CRAP >= gate")
	all := flag.Bool("all", false, "print every function, not just gate failures")
	flag.Parse()

	fns, err := scanModule()
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

	type row struct {
		fn   fn
		cov  float64
		crap float64
	}
	var rows []row
	var failing []row
	for _, f := range fns {
		cov := 0.0
		if f.stmts > 0 {
			cov = float64(f.covered) / float64(f.stmts)
		}
		c := float64(f.complexity)
		crap := c*c*(1-cov)*(1-cov)*(1-cov) + c
		r := row{f, cov, crap}
		rows = append(rows, r)
		if crap >= *gate && f.name != "main" {
			failing = append(failing, r)
		}
	}
	sort.SliceStable(failing, func(i, j int) bool { return failing[i].crap > failing[j].crap })

	fmt.Printf("%-28s %4s %7s %9s\n", "FUNCTION", "CC", "COV", "CRAP")
	fmt.Println(strings.Repeat("-", 54))
	for _, r := range failing {
		fmt.Printf("%-28s %4d %6.1f%% %9.2f  (score >= %.0f)\n",
			displayName(r.fn), r.fn.complexity, r.cov*100, r.crap, *gate)
	}
	if *all {
		fmt.Println()
		for _, r := range rows {
			fmt.Printf("ALL\t%s\t%d\t%.1f\t%.2f\n", displayName(r.fn), r.fn.complexity, r.cov*100, r.crap)
		}
	}
	if len(failing) == 0 {
		fmt.Printf("CRAP gate green: no function (main excluded) reaches %.0f.\n", *gate)
	}
	fmt.Printf("\n%d functions scored, %d fail the CRAP < %.0f gate (main excluded).\n",
		len(rows), len(failing), *gate)
}

func displayName(f fn) string {
	base := filepath.Base(f.file)
	if f.name == "main" {
		return base + ": main()"
	}
	return fmt.Sprintf("%s: %s (L%d)", base, f.name, f.line)
}

// scanModule parses every non-test .go file and records each named function.
func scanModule() ([]fn, error) {
	var fns []fn
	root := "."
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

// complexityOf returns the cyclomatic complexity of the function, including
// nested function literals.
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
	defer fh.Close()
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

// attach sums profile statements into the function whose line range contains
// each block. File matching is by basename-without-suffix, robust to the
// module-qualified paths that go test -coverprofile emits.
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
