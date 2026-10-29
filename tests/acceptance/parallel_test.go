package acceptance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// swapsTheKey is why a test that runs under another token stays serial: t.Parallel panics after
// t.Setenv, and COSTFLUENT_API_KEY is process-wide in any case, so a parallel test beside it would
// run under the wrong token.
const swapsTheKey = "swaps COSTFLUENT_API_KEY with t.Setenv"

// serialAcceptanceTests names every acceptance test that calls resource.Test rather than
// resource.ParallelTest, with the reason it cannot run beside the others. Go finishes every serial
// top-level test before it releases the parallel ones, so the two kinds never overlap.
var serialAcceptanceTests = map[string]string{
	"TestAccApp_email":                                 swapsTheKey,
	"TestAccApp_webhookGeneratedSecret":                swapsTheKey,
	"TestAccApp_webhookNullSecretKeepsTheGeneratedOne": swapsTheKey,
	"TestAccApp_webhookSecretRemoved":                  swapsTheKey,
	"TestAccTeam_withWorkspaceAccess":                  swapsTheKey,
	"TestAccTeamMember_lookedUpByEmail":                swapsTheKey,
	"TestAccAnomalies_readTheSeededAnomalies":          swapsTheKey,
	"TestAccCommitments_readTheSeededCommitments":      swapsTheKey,
	"TestAccSavingsItems_readTheSeededFindings":        swapsTheKey,
}

const resourcePackagePath = "github.com/hashicorp/terraform-plugin-testing/helper/resource"

// TestAcceptanceTestsRunInParallel keeps the suite parallel by default: a new acceptance test runs
// beside the others unless serialAcceptanceTests says why it cannot. It reads the source rather than
// running anything, so it needs no TF_ACC and fails in the unit run, before an acceptance run would
// panic on a parallel test that swaps the key.
func TestAcceptanceTestsRunInParallel(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	sources := make(map[string]string, len(files))
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sources[file] = string(src)
	}
	problems, err := checkParallelAcceptanceTests(sources, serialAcceptanceTests)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestParallelGuard(t *testing.T) {
	const header = `package acceptance

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func useOtherKey(t *testing.T) { t.Setenv("COSTFLUENT_API_KEY", "other") }

func useOtherKeyIndirectly(t *testing.T) { useOtherKey(t) }
`
	tests := []struct {
		name   string
		source string
		serial map[string]string
		want   []string
	}{
		{
			name: "a clean file is accepted",
			source: header + `
func TestAccParallel(t *testing.T) { resource.ParallelTest(t, resource.TestCase{}) }

func TestAccSerial(t *testing.T) {
	useOtherKey(t)
	resource.Test(t, resource.TestCase{})
}

func TestPlain(t *testing.T) { t.Setenv("TF_LOG", "DEBUG") }
`,
			serial: map[string]string{"TestAccSerial": swapsTheKey},
		},
		{
			name:   "an unlisted serial test is refused",
			source: header + "\nfunc TestAccSerial(t *testing.T) { resource.Test(t, resource.TestCase{}) }\n",
			want:   []string{"TestAccSerial calls resource.Test"},
		},
		{
			name:   "an entry whose test is gone is stale",
			source: header + "\nfunc TestAccParallel(t *testing.T) { resource.ParallelTest(t, resource.TestCase{}) }\n",
			serial: map[string]string{"TestAccGone": swapsTheKey},
			want:   []string{"TestAccGone is in serialAcceptanceTests but no function calls resource.Test"},
		},
		{
			name:   "an entry whose test now runs in parallel is stale",
			source: header + "\nfunc TestAccParallel(t *testing.T) { resource.ParallelTest(t, resource.TestCase{}) }\n",
			serial: map[string]string{"TestAccParallel": "was serial"},
			want: []string{
				"TestAccParallel is in serialAcceptanceTests but no function calls resource.Test",
			},
		},
		{
			name: "a parallel test that sets the environment is refused",
			source: header + `
func TestAccParallel(t *testing.T) {
	t.Setenv("COSTFLUENT_API_KEY", "other")
	resource.ParallelTest(t, resource.TestCase{})
}
`,
			want: []string{"TestAccParallel calls resource.ParallelTest after swapping the environment"},
		},
		{
			name: "a parallel test that swaps the key through a helper is refused",
			source: header + `
func TestAccParallel(t *testing.T) {
	useOtherKeyIndirectly(t)
	resource.ParallelTest(t, resource.TestCase{})
}
`,
			want: []string{"TestAccParallel calls resource.ParallelTest after swapping the environment"},
		},
		{
			name: "an aliased import is still read",
			source: `package acceptance

import (
	"testing"

	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccSerial(t *testing.T) { tfresource.Test(t, tfresource.TestCase{}) }
`,
			want: []string{"TestAccSerial calls resource.Test"},
		},
		{
			name:   "a scan that finds no acceptance test fails",
			source: "package acceptance\n",
			want:   []string{"found no function calling resource.Test or resource.ParallelTest"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems, err := checkParallelAcceptanceTests(map[string]string{"case_test.go": tt.source}, tt.serial)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != len(tt.want) {
				t.Fatalf("got %d problems %q, want %d matching %q", len(problems), problems, len(tt.want), tt.want)
			}
			for i, want := range tt.want {
				if !strings.Contains(problems[i], want) {
					t.Errorf("problem %d = %q, want it to contain %q", i, problems[i], want)
				}
			}
		})
	}

	if _, err := checkParallelAcceptanceTests(map[string]string{"broken_test.go": "package acceptance\nfunc {"}, nil); err == nil {
		t.Error("a file that does not parse was accepted")
	}
}

// acceptanceFunc is what the guard reads from one top-level function.
type acceptanceFunc struct {
	file     string
	serial   bool // calls resource.Test
	parallel bool // calls resource.ParallelTest
	calls    []string
	setsEnv  bool // calls Setenv itself
}

// checkParallelAcceptanceTests reads the package's test sources, file name to text, and returns
// each way they break the parallel rule, sorted, against serial: the tests allowed to call
// resource.Test.
func checkParallelAcceptanceTests(sources map[string]string, serial map[string]string) ([]string, error) {
	fset := token.NewFileSet()
	funcs := map[string]*acceptanceFunc{}
	for name, src := range sources {
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		resourceName := ""
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == resourcePackagePath {
				resourceName = "resource"
				if imp.Name != nil {
					resourceName = imp.Name.Name
				}
			}
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			read := &acceptanceFunc{file: name}
			funcs[fn.Name.Name] = read
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					read.calls = append(read.calls, fun.Name)
				case *ast.SelectorExpr:
					if pkg, ok := fun.X.(*ast.Ident); ok && resourceName != "" && pkg.Name == resourceName {
						read.serial = read.serial || fun.Sel.Name == "Test"
						read.parallel = read.parallel || fun.Sel.Name == "ParallelTest"
					}
					read.setsEnv = read.setsEnv || fun.Sel.Name == "Setenv"
				}
				return true
			})
		}
	}

	// A function swaps the environment when it calls Setenv or a package function that does.
	swaps := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for name, fn := range funcs {
			if swaps[name] {
				continue
			}
			if fn.setsEnv || slices.ContainsFunc(fn.calls, func(callee string) bool { return swaps[callee] }) {
				swaps[name] = true
				changed = true
			}
		}
	}

	var problems []string
	found := 0
	for name, fn := range funcs {
		if fn.serial || fn.parallel {
			found++
		}
		if fn.serial {
			if _, listed := serial[name]; !listed {
				problems = append(problems, fmt.Sprintf("%s: %s calls resource.Test: make it resource.ParallelTest, or add it to serialAcceptanceTests with the reason it cannot run beside the others", fn.file, name))
			}
		}
		if fn.parallel && swaps[name] {
			problems = append(problems, fmt.Sprintf("%s: %s calls resource.ParallelTest after swapping the environment, which panics under TF_ACC and would change the token of every test beside it: make it resource.Test and add it to serialAcceptanceTests", fn.file, name))
		}
	}
	for name := range serial {
		if fn, ok := funcs[name]; !ok || !fn.serial {
			problems = append(problems, fmt.Sprintf("%s is in serialAcceptanceTests but no function calls resource.Test under that name: remove the entry", name))
		}
	}
	if found == 0 {
		problems = append(problems, "found no function calling resource.Test or resource.ParallelTest: the scan no longer sees the suite")
	}
	slices.Sort(problems)
	return problems, nil
}
