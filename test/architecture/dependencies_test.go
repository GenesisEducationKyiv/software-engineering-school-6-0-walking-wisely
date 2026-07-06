package architecture

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestArchitectureDependencies(t *testing.T) {
	modPath := modulePath(t)
	root := moduleRoot(t)

	allDeps := listAllDeps(t, root)

	for _, rule := range Rules {
		t.Run(rule.Name, func(t *testing.T) {
			deps, ok := allDeps[rule.Package]
			if !ok {
				t.Fatalf("package %s not found in go list output", rule.Package)
			}

			var violations []string
			for _, dep := range deps {
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

type pkgJSON struct {
	ImportPath string   `json:"ImportPath"`
	Deps       []string `json:"Deps"`
}

func listAllDeps(t *testing.T, root string) map[string][]string {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "./internal/...", "./gen/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -json failed: %v", err)
	}

	result := make(map[string][]string)
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg pkgJSON
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("failed to decode go list output: %v", err)
		}
		result[pkg.ImportPath] = pkg.Deps
	}
	return result
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func modulePath(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m failed: %v", err)
	}
	return strings.TrimSpace(string(out))
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
