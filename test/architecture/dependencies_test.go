package architecture

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestArchitectureDependencies(t *testing.T) {
	modPath, modDir := moduleInfo(t)

	byPath := loadPackages(t, modDir)

	for _, rule := range Rules {
		t.Run(rule.Name, func(t *testing.T) {
			pkg, ok := byPath[rule.Package]
			if !ok {
				t.Fatalf("package %s not found in loaded packages", rule.Package)
			}

			deps := transitiveDeps(pkg)

			var violations []string
			for dep := range deps {
				if dep == rule.Package {
					continue
				}
				if !isInternal(dep, modPath) {
					continue
				}
				if !isAllowed(dep, rule.AllowedPrefixes) {
					violations = append(violations, dep)
				}
			}

			if len(violations) > 0 {
				t.Errorf("rule %s failed:\npackage: %s\nforbidden dependencies:\n",
					rule.Name, rule.Package)
				for _, v := range violations {
					t.Errorf("  - %s", v)
				}
				t.Errorf("allowed prefixes:")
				for _, p := range rule.AllowedPrefixes {
					t.Errorf("  - %s", p)
				}
			}
		})
	}
}

// moduleInfo resolves the main module's import path and root directory without
// shelling out to `go list -m`, using the same packages.Load driver as the rest
// of this file so behavior stays consistent across Go versions/environments.
func moduleInfo(t *testing.T) (path, dir string) {
	t.Helper()

	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedModule}, ".")
	if err != nil {
		t.Fatalf("failed to resolve module: %v", err)
	}
	if len(pkgs) == 0 || pkgs[0].Module == nil {
		t.Fatalf("failed to resolve module: no module info returned")
	}
	return pkgs[0].Module.Path, pkgs[0].Module.Dir
}

// loadPackages loads the full import graph under internal/ and gen/ and returns
// a map keyed by package import path. Errors from the load itself (missing
// packages, e.g. gen/ not yet generated via `make generate`, or broken imports)
// are surfaced as clear test failures instead of an opaque process exit code.
func loadPackages(t *testing.T, modDir string) map[string]*packages.Package {
	t.Helper()

	cfg := &packages.Config{
		Dir:  modDir,
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps,
	}
	pkgs, err := packages.Load(cfg, "./internal/...", "./gen/...")
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	if errCount := packages.PrintErrors(pkgs); errCount > 0 {
		t.Fatalf("go/packages reported %d error(s) while loading ./internal/... and ./gen/...; "+
			"if this mentions missing gen/ packages, run `make generate` first", errCount)
	}

	byPath := make(map[string]*packages.Package)
	for _, pkg := range pkgs {
		byPath[pkg.PkgPath] = pkg
	}
	return byPath
}

// transitiveDeps walks the import graph reachable from root and returns the
// set of all transitively imported package paths, mirroring what `go list
// -json`'s flattened .Deps field provides directly.
func transitiveDeps(root *packages.Package) map[string]bool {
	seen := make(map[string]bool)

	var walk func(p *packages.Package)
	walk = func(p *packages.Package) {
		for path, imp := range p.Imports {
			if seen[path] {
				continue
			}
			seen[path] = true
			walk(imp)
		}
	}
	walk(root)

	return seen
}

func isInternal(dep, modPath string) bool {
	return strings.HasPrefix(dep, modPath+"/")
}

func isAllowed(dep string, allowedPrefixes []string) bool {
	for _, prefix := range allowedPrefixes {
		if strings.HasSuffix(prefix, "/") {
			root := strings.TrimSuffix(prefix, "/")
			if dep == root || strings.HasPrefix(dep, prefix) {
				return true
			}
		} else if dep == prefix {
			return true
		}
	}
	return false
}
