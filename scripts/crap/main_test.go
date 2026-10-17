package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"testing"
)

// parseFuncs parses a source snippet and returns its named function
// declarations, for exercising complexityOf on synthetic code.
func parseFuncs(t *testing.T, src string) []*ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var decls []*ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			decls = append(decls, fd)
		}
	}
	return decls
}

// Reference values cross-checked against PHPUnit's CrapIndex on the
// research branch (docs/research/crap-tooling.md): (2, 0%) = 6,
// (3, 100%) = 3, (4, 50%) = 6.00.
func TestCrapScore_MatchesPHPUnitReferenceValues(t *testing.T) {
	cases := []struct {
		name string
		comp int
		cov  float64
		want float64
	}{
		{"comp 2 uncovered", 2, 0.0, 6.0},
		{"comp 3 fully covered", 3, 1.0, 3.0},
		{"comp 4 half covered", 4, 0.5, 6.0},
		{"comp 1 always passes", 1, 0.0, 2.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := crapScore(tc.comp, tc.cov)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("crapScore(%d, %.3f) = %.4f, want %.4f", tc.comp, tc.cov, got, tc.want)
			}
		})
	}
}

// Coverage >= 95% counts as fully covered (PHPUnit clamp), so CRAP
// collapses to the complexity. ParticipantStore.Change: CC 20 at 97.1%
// coverage scored 20.01 without the clamp on the research branch; with
// the clamp it is exactly 20.
func TestCrapScore_ClampsHighCoverageToFullyCovered(t *testing.T) {
	cases := []struct {
		name string
		comp int
		cov  float64
		want float64
	}{
		{"Change at 97.1%", 20, 0.971, 20.0},
		{"just at the clamp", 10, 0.95, 10.0},
		{"above the clamp", 10, 0.96, 10.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := crapScore(tc.comp, tc.cov)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("crapScore(%d, %.3f) = %.4f, want %.4f", tc.comp, tc.cov, got, tc.want)
			}
		})
	}
}

// Below the clamp the uncertainty term is real: 10.0132651 =
// 100 * (1 - 0.949)^3 + 10.
func TestCrapScore_BelowClampKeepsUncertaintyTerm(t *testing.T) {
	got := crapScore(10, 0.949)
	want := 10.0132651
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("crapScore(10, 0.949) = %.7f, want %.7f", got, want)
	}
}

// gocyclo convention: base 1, +1 per if/for/range/case/comm-clause,
// +1 per && and ||, and no +1 for default clauses. The default-clause
// convention is the one ADR-0023 adopts against crap4go's +1.
func TestComplexityOf_GocycloConvention(t *testing.T) {
	src := `package p

func kitchen(items []string, flag bool) int {
	n := 0
	if flag {
		n++
	}
	for i := 0; i < 3; i++ {
		n++
	}
	for range items {
		n++
	}
	switch n {
	case 1:
		n++
	case 2:
		n++
	default:
		n = 0
	}
	if flag && n > 0 || n < 0 {
		n++
	}
	return n
}
`
	// 1 base + if + for + range + 2 cases + if + && + || = 9; default is free.
	if got := complexityOf(parseFuncs(t, src)[0]); got != 9 {
		t.Errorf("complexityOf(kitchen) = %d, want 9", got)
	}
}

func TestComplexityOf_DefaultClauseIsFree(t *testing.T) {
	src := `package p

func pick(s string) int {
	switch s {
	case "a":
		return 1
	default:
		return 0
	}
}
`
	// 1 base + 1 case; the default clause adds nothing (gocyclo).
	if got := complexityOf(parseFuncs(t, src)[0]); got != 2 {
		t.Errorf("complexityOf(pick) = %d, want 2", got)
	}
}

// Function literals are attributed to the enclosing named function, so
// closures cannot hide complexity from the gate.
func TestComplexityOf_FunctionLiteralAttributedToEnclosingFunction(t *testing.T) {
	src := `package p

func outer(xs []int) int {
	abs := func(x int) int {
		if x > 0 {
			return x
		}
		return -x
	}
	return abs(len(xs))
}
`
	// 1 base + the closure's if = 2.
	if got := complexityOf(parseFuncs(t, src)[0]); got != 2 {
		t.Errorf("complexityOf(outer) = %d, want 2", got)
	}
}

// Coverage comes from the profile blocks summed into the function:
// 97 of 100 statements covered is 0.97, which the clamp treats as
// fully covered; a function with no profile blocks counts as
// uncovered (cov 0).
func TestCoverageFraction(t *testing.T) {
	cases := []struct {
		name           string
		stmts, covered int
		want           float64
	}{
		{"97 of 100", 100, 97, 0.97},
		{"no profile blocks", 0, 0, 0.0},
		{"fully covered", 8, 8, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{stmts: tc.stmts, covered: tc.covered}
			if got := coverageOf(f); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("coverageOf(%d/%d) = %.4f, want %.4f", tc.covered, tc.stmts, got, tc.want)
			}
		})
	}
}

// main() is excluded from the gate exactly as ADR-0008 excludes it
// from the coverage gate: it is scored, but never counts as failing.
func TestScoreFunctions_ExcludesMain(t *testing.T) {
	fns := []fn{
		{name: "main", complexity: 10, stmts: 10, covered: 10},
		{name: "Handler.Meet", complexity: 7, stmts: 10, covered: 10},
		{name: "trimLink", complexity: 5, stmts: 10, covered: 10},
	}
	all, failing := scoreFunctions(fns, 6.0)
	if len(all) != 3 {
		t.Errorf("scored %d functions, want 3", len(all))
	}
	if len(failing) != 1 {
		t.Fatalf("failing count = %d, want 1 (Handler.Meet only)", len(failing))
	}
	if failing[0].fn.name != "Handler.Meet" {
		t.Errorf("failing[0] = %s, want Handler.Meet", failing[0].fn.name)
	}
}

func TestScoreFunctions_WorstFirst(t *testing.T) {
	fns := []fn{
		{name: "a", complexity: 6, stmts: 10, covered: 10},
		{name: "b", complexity: 30, stmts: 10, covered: 10},
		{name: "c", complexity: 7, stmts: 10, covered: 10},
	}
	_, failing := scoreFunctions(fns, 6.0)
	if len(failing) != 3 || failing[0].fn.name != "b" || failing[1].fn.name != "c" || failing[2].fn.name != "a" {
		var order []string
		for _, f := range failing {
			order = append(order, f.fn.name)
		}
		t.Errorf("failing order = %v, want [b c a]", order)
	}
}
