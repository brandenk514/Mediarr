// Command speccheck validates openapi.yaml against the OpenAPI 3.0 spec.
//
// It is the implementation behind `make spec-check` and the loading path the
// spec-drift test (internal/api) reuses: kin-openapi parses the document and
// runs the full structural validation ($refs, response schemas, parameters,
// security schemes), so a broken spec fails fast locally instead of in CI.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/getkin/kin-openapi/openapi3"
)

func main() {
	path := "openapi.yaml"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		fatalf("resolve %s: %v", path, err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(abs)
	if err != nil {
		fatalf("parse %s: %v", path, err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		fatalf("validate %s: %v", path, err)
	}

	ops, schemas := 0, len(doc.Components.Schemas)
	for _, p := range doc.Paths.Map() {
		ops += len(p.Operations())
	}
	fmt.Printf("openapi.yaml OK: %d paths, %d operations, %d component schemas\n",
		len(doc.Paths.Map()), ops, schemas)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "speccheck: "+format+"\n", args...)
	os.Exit(1)
}
