package architecture

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUnnecessaryImportAliases(t *testing.T) {
	root := moduleRoot(t)
	modPath := modulePath(t)

	pkgNameMap := buildPkgNameMap(t, root, modPath)

	var allViolations []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == ".git" || d.Name() == "gen" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		fileViolations := checkImportAliases(path, root, pkgNameMap)
		allViolations = append(allViolations, fileViolations...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}

	if len(allViolations) > 0 {
		t.Errorf("unnecessary import aliases found:\n%s", strings.Join(allViolations, "\n"))
	}
}

type importInfo struct {
	alias       string
	defaultName string
	pkgPath     string
}

var knownPkgNames = map[string]string{
	"github.com/redis/go-redis/v9":          "redis",
	"github.com/prometheus/client_model/go": "dto",
}

func buildPkgNameMap(t *testing.T, root, modPath string) map[string]string {
	t.Helper()

	m := make(map[string]string)

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, _ := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if f == nil {
			return nil
		}

		rel, _ := filepath.Rel(root, filepath.Dir(path))

		importPath := modPath + "/" + rel
		if _, ok := m[importPath]; !ok {
			m[importPath] = f.Name.Name
		}
		return nil
	})

	return m
}

func defaultPkgName(pkgPath string, pkgNameMap map[string]string) string {
	if name, ok := pkgNameMap[pkgPath]; ok {
		return name
	}
	return heuristicPkgName(pkgPath)
}

// rawHeuristic computes the package name that goimports would infer from the
// import path alone (last segment, version stripped), without any known-name
// overrides. This is the baseline that goimports uses when the package's
// .go files are not available in the module cache.
func rawHeuristic(pkgPath string) string {
	segs := strings.Split(pkgPath, "/")
	last := segs[len(segs)-1]
	if len(segs) >= 2 && isVersion(last) {
		last = segs[len(segs)-2]
	}
	last = strings.TrimSuffix(last, ".go")
	return last
}

// heuristicPkgName computes the Go package name from the import path alone,
// without consulting the actual source. This is what goimports uses.
func heuristicPkgName(pkgPath string) string {
	if name, ok := knownPkgNames[pkgPath]; ok {
		return name
	}
	segs := strings.Split(pkgPath, "/")
	last := segs[len(segs)-1]

	if len(segs) >= 2 && isVersion(last) {
		last = segs[len(segs)-2]
	}
	last = strings.TrimSuffix(last, ".go")
	return last
}

func isVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	_, err := strconv.Atoi(s[1:])
	return err == nil
}

func checkImportAliases(path, root string, pkgNameMap map[string]string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return []string{fmt.Sprintf("  %s: parse error: %v", relPath(path, root), err)}
	}

	imports := f.Imports
	if len(imports) == 0 {
		return nil
	}

	pkgName := f.Name.Name

	var infos []importInfo
	defaultNames := make(map[string]int)

	for _, imp := range imports {
		pkgPath := strings.Trim(imp.Path.Value, "\"")
		var alias string
		if imp.Name != nil {
			alias = imp.Name.Name
		}

		if alias == "_" || alias == "." {
			continue
		}

		dn := defaultPkgName(pkgPath, pkgNameMap)

		infos = append(infos, importInfo{alias, dn, pkgPath})
		defaultNames[dn]++
	}

	var violations []string
	for _, info := range infos {
		if info.alias == "" {
			continue
		}

		if isAllowedAlias(info.pkgPath, info.alias) {
			continue
		}

		if info.defaultName == pkgName {
			continue
		}

		// The alias is unnecessary only if goimports would infer the same name
		// from the path alone (i.e. the raw heuristic, without known exceptions).
		// If the alias differs from the heuristic, goimports would keep it,
		// making it meaningful.
		if info.alias != rawHeuristic(info.pkgPath) {
			continue
		}

		if defaultNames[info.defaultName] <= 1 {
			violations = append(violations, fmt.Sprintf("  %s: unnecessary alias %q for %q (default name %q is unique in file)",
				relPath(path, root), info.alias, info.pkgPath, info.defaultName))
		}
	}

	return violations
}

func relPath(path, root string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}

func isAllowedAlias(pkgPath, alias string) bool {
	for _, rule := range AliasAllowRules {
		if rule.PkgPath == pkgPath && (rule.Alias == "" || rule.Alias == alias) {
			return true
		}
	}
	return false
}
