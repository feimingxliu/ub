package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/invopop/jsonschema"
)

func main() {
	reflector := jsonschema.Reflector{
		ExpandedStruct: true,
	}
	schema := reflector.Reflect(&config.Config{})

	out, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal schema: %v\n", err)
		os.Exit(1)
	}
	out = append(out, '\n')

	if err := os.MkdirAll("api", 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create api dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join("api", "config.schema.json"), out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write schema: %v\n", err)
		os.Exit(1)
	}
}
