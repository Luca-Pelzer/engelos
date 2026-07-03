// SPDX-License-Identifier: Apache-2.0

package sdk_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestSDK_NoInternalImports enforces the SDK's core invariant: neither pkg/sdk
// nor its examples may import anything under internal/, so a third-party author
// compiles against a stable public surface. The test walks the SDK source tree
// (its own dir plus examples/) and fails on any offending import path.
func TestSDK_NoInternalImports(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "/internal/") || strings.HasSuffix(p, "/internal") {
				t.Errorf("%s imports internal path %q — pkg/sdk must not depend on internal/", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk sdk sources: %v", err)
	}
}
