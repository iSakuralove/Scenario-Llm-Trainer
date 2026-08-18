package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	workingDir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	source := filepath.Clean(filepath.Join(
		workingDir,
		"..", "..", "..",
		"agent", "src", "hiddenworld", "bank", "fixed", "hw-db-index-001.json",
	))
	destinationDir := filepath.Join(workingDir, "fixed_hiddenworld")
	destination := filepath.Join(destinationDir, "hw-db-index-001.json")

	raw, err := os.ReadFile(source)
	if err != nil {
		panic(fmt.Errorf("read source bank file: %w", err))
	}
	if !json.Valid(raw) {
		panic(fmt.Errorf("source bank file is not valid JSON: %s", source))
	}
	raw = append(bytes.TrimSpace(raw), '\n')
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		panic(fmt.Errorf("create generated bank directory: %w", err))
	}
	if err := os.WriteFile(destination, raw, 0o644); err != nil {
		panic(fmt.Errorf("write generated bank file: %w", err))
	}
	fmt.Printf("synced %s -> %s\n", source, destination)
}
