package acceptance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// needsARealCloud is why a test may skip on its own variables under CI too: CI never connects a
// real cloud account, so the variables naming one are never set there.
const needsARealCloud = "needs a real GCP project and billing export, which CI never connects"

// skippingAcceptanceTests names every function that may call t.Skip outside testAccRequireEnv and
// an unset-TF_ACC branch, with the reason its skip is not a lost runner input.
var skippingAcceptanceTests = map[string]string{
	"TestAccGcpServiceAccount_basic": needsARealCloud,
	"TestAccProvider_gcp":            needsARealCloud,
}

// skipHelper is the one function whose skips need no reason: it skips only where no acceptance
// run was asked for or outside CI, and fails under CI.
const skipHelper = "testAccRequireEnv"

// TestAcceptanceSkipsFailUnderCI keeps a lost runner input from turning acceptance tests into
// skips with the suite green: a test that needs a value beyond COSTFLUENT_API_KEY reads it through
// testAccRequireEnv, which fails under CI, unless skippingAcceptanceTests says why it may skip. It
// reads the source rather than running anything, so it needs no TF_ACC and fails in the unit run.
func TestAcceptanceSkipsFailUnderCI(t *testing.T) {
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
	problems, err := checkAcceptanceSkips(sources, skippingAcceptanceTests)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestSkipGuard(t *testing.T) {
	const header = `package acceptance

import (
	"os"
	"testing"
)

func testAccRequireEnv(t *testing.T, name, what string) string {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests run with TF_ACC set")
	}
	if os.Getenv(name) == "" {
		t.Skipf("%s is unset", name)
	}
	return os.Getenv(name)
}
`
	tests := []struct {
		name    string
		source  string
		allowed map[string]string
		want    []string
	}{
		{
			name: "a clean file is accepted",
			source: header + `
func TestAccNeedsTFAcc(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests run with TF_ACC set")
	}
}

func TestAccReversed(t *testing.T) {
	if "" == os.Getenv("TF_ACC") {
		t.SkipNow()
	}
}

func TestAccRawStrings(t *testing.T) {
	if os.Getenv(` + "`TF_ACC`) == `` {" + `
		t.Skip("acceptance tests run with TF_ACC set")
	}
}

func TestAccCloud(t *testing.T) {
	if os.Getenv("COSTFLUENT_ACC_GCP_PROJECT_ID") == "" {
		t.Skip("COSTFLUENT_ACC_GCP_PROJECT_ID must be set")
	}
}

func linkedElsewhere(t *testing.T)
`,
			allowed: map[string]string{"TestAccCloud": needsARealCloud},
		},
		{
			name: "a bare skip on an empty variable is refused",
			source: header + `
func testAccUseAdminKey(t *testing.T) {
	key := os.Getenv("COSTFLUENT_ACC_ADMIN_API_KEY")
	if key == "" {
		t.Skip("COSTFLUENT_ACC_ADMIN_API_KEY holds no token")
	}
	t.Setenv("COSTFLUENT_API_KEY", key)
}
`,
			want: []string{"testAccUseAdminKey calls t.Skip"},
		},
		{
			name: "Skipf and SkipNow are refused too",
			source: header + `
func TestAccA(t *testing.T) {
	if os.Getenv("COSTFLUENT_ACC_APP_ID") == "" {
		t.Skipf("%s", "no app")
	}
}

func TestAccB(t *testing.T) { t.SkipNow() }
`,
			want: []string{"TestAccA calls t.Skipf", "TestAccB calls t.SkipNow"},
		},
		{
			name: "a skip in a closure is the enclosing function's",
			source: header + `
func TestAccClosure(t *testing.T) {
	check := func() { t.Skip("later") }
	check()
}
`,
			want: []string{"TestAccClosure calls t.Skip"},
		},
		{
			name: "only the unset-TF_ACC branch is exempt",
			source: header + `
func TestAccElse(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		return
	} else {
		t.Skip("in the else branch")
	}
}

func TestAccSet(t *testing.T) {
	if os.Getenv("TF_ACC") != "" {
		t.Skip("TF_ACC is set")
	}
}

func TestAccOther(t *testing.T) {
	if os.Getenv("CI") == "" {
		t.Skip("not CI")
	}
}

func TestAccNotOs(t *testing.T) {
	if env.Getenv("TF_ACC") == "" {
		t.Skip("another package's Getenv")
	}
}

func TestAccNotGetenv(t *testing.T) {
	if os.ExpandEnv("TF_ACC") == "" {
		t.Skip("not Getenv")
	}
}

func TestAccNoArgument(t *testing.T) {
	if os.Getenv() == "" {
		t.Skip("no argument")
	}
}
`,
			want: []string{
				"TestAccElse calls t.Skip", "TestAccSet calls t.Skip", "TestAccOther calls t.Skip",
				"TestAccNotOs calls t.Skip", "TestAccNotGetenv calls t.Skip", "TestAccNoArgument calls t.Skip",
			},
		},
		{
			name: "an entry whose function no longer skips is stale",
			source: header + `
func TestAccCloud(t *testing.T) {}
`,
			allowed: map[string]string{"TestAccCloud": needsARealCloud, "TestAccGone": needsARealCloud},
			want: []string{
				"TestAccCloud is in skippingAcceptanceTests but calls no t.Skip",
				"TestAccGone is in skippingAcceptanceTests but calls no t.Skip",
			},
		},
		{
			name:   "a scan that finds no skip fails",
			source: "package acceptance\n",
			want:   []string{"found no t.Skip call"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems, err := checkAcceptanceSkips(map[string]string{"case_test.go": tt.source}, tt.allowed)
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

	if _, err := checkAcceptanceSkips(map[string]string{"broken_test.go": "package acceptance\nfunc {"}, nil); err == nil {
		t.Error("a file that does not parse was accepted")
	}
}

// endingTB records how testAccRequireEnv ended the test it was handed. Like testing.T, its Fatalf
// and Skipf stop the calling goroutine, so code after them never runs.
type endingTB struct {
	testing.TB
	fatal, skip string
}

func (e *endingTB) Helper() {}

func (e *endingTB) Fatalf(format string, args ...any) {
	e.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (e *endingTB) Skip(args ...any) {
	e.skip = fmt.Sprint(args...)
	runtime.Goexit()
}

func (e *endingTB) Skipf(format string, args ...any) {
	e.skip = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// TestRequireEnv pins the helper the skip guard sends every acceptance input through: it fails
// under CI when the runner did not pass the value, and skips only without TF_ACC or outside CI.
// Any non-empty CI or TF_ACC counts as set, as the testing framework reads TF_ACC.
func TestRequireEnv(t *testing.T) {
	const name = "COSTFLUENT_ACC_REQUIRE_ENV_PROBE"
	tests := []struct {
		name              string
		tfAcc, ci, value  string
		want, fatal, skip string
	}{
		{name: "without TF_ACC it skips, under CI too", ci: "true", skip: "acceptance tests run with TF_ACC set"},
		{name: "a passed value is returned", tfAcc: "1", ci: "true", value: "probe-value", want: "probe-value"},
		{name: "a lost value fails under CI", tfAcc: "1", ci: "true", fatal: name + " must hold the probe under CI"},
		{name: "a lost value skips outside CI", tfAcc: "1", skip: name + " is unset, and the test needs the probe"},
		{name: "any non-empty CI counts as CI", tfAcc: "1", ci: "1", fatal: name + " must hold the probe under CI"},
		{name: "any non-empty TF_ACC runs the test", tfAcc: "true", ci: "true", value: "probe-value", want: "probe-value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TF_ACC", tt.tfAcc)
			t.Setenv("CI", tt.ci)
			t.Setenv(name, tt.value)
			ended := &endingTB{}
			got := ""
			done := make(chan struct{})
			go func() {
				defer close(done)
				got = testAccRequireEnv(ended, name, "the probe")
			}()
			<-done
			if got != tt.want || ended.fatal != tt.fatal || ended.skip != tt.skip {
				t.Errorf("got value %q, fatal %q, skip %q; want value %q, fatal %q, skip %q", got, ended.fatal, ended.skip, tt.want, tt.fatal, tt.skip)
			}
		})
	}
}

var skipMethods = []string{"Skip", "Skipf", "SkipNow"}

// checkAcceptanceSkips reads the package's test sources, file name to text, and returns, sorted,
// each skip call outside skipHelper, an unset-TF_ACC branch and the functions allowed names, and
// each entry of allowed whose function no longer skips.
func checkAcceptanceSkips(sources map[string]string, allowed map[string]string) ([]string, error) {
	fset := token.NewFileSet()
	skipping := map[string]bool{}
	var problems []string
	found := 0
	for name, src := range sources {
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var exempt []*ast.BlockStmt
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if stmt, ok := n.(*ast.IfStmt); ok && isUnsetTFAcc(stmt.Cond) {
					exempt = append(exempt, stmt.Body)
				}
				return true
			})
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !slices.Contains(skipMethods, sel.Sel.Name) {
					return true
				}
				found++
				if slices.ContainsFunc(exempt, func(b *ast.BlockStmt) bool { return b.Pos() <= call.Pos() && call.End() <= b.End() }) {
					return true
				}
				skipping[fn.Name.Name] = true
				if _, ok := allowed[fn.Name.Name]; ok || fn.Name.Name == skipHelper {
					return true
				}
				problems = append(problems, fmt.Sprintf("%s: %s calls t.%s, which leaves the suite green when the runner stops passing a value: read the value with testAccRequireEnv, which fails under CI, or add the function to skippingAcceptanceTests with the reason it may skip there", fset.Position(call.Pos()), fn.Name.Name, sel.Sel.Name))
				return true
			})
		}
	}
	for name := range allowed {
		if !skipping[name] {
			problems = append(problems, fmt.Sprintf("%s is in skippingAcceptanceTests but calls no t.Skip outside an unset-TF_ACC branch: remove the entry", name))
		}
	}
	if found == 0 {
		problems = append(problems, "found no t.Skip call: the scan no longer sees the suite")
	}
	slices.Sort(problems)
	return problems, nil
}

// isUnsetTFAcc reports whether cond is os.Getenv("TF_ACC") == "", either way round.
func isUnsetTFAcc(cond ast.Expr) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL {
		return false
	}
	return (isGetenvTFAcc(bin.X) && isEmptyString(bin.Y)) || (isEmptyString(bin.X) && isGetenvTFAcc(bin.Y))
}

func isGetenvTFAcc(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Getenv" {
		return false
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	return err == nil && value == "TF_ACC"
}

func isEmptyString(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && (lit.Value == `""` || lit.Value == "``")
}
