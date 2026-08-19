package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var fixedBankIDs = []string{
	"hw-db-index-001",
	"hw-network-vip-001",
	"hw-k8s-io-001",
	"hw-cache-key-001",
}

func main() {
	workingDir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	destinationDir := filepath.Join(workingDir, "fixed_hiddenworld")
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		panic(fmt.Errorf("create generated bank directory: %w", err))
	}
	for _, id := range fixedBankIDs {
		source := filepath.Clean(filepath.Join(
			workingDir,
			"..", "..", "..",
			"agent", "src", "hiddenworld", "bank", "fixed", id+".json",
		))
		destination := filepath.Join(destinationDir, id+".json")
		raw, err := os.ReadFile(source)
		if err != nil {
			panic(fmt.Errorf("read source bank file %s: %w", id, err))
		}
		if !json.Valid(raw) {
			panic(fmt.Errorf("source bank file is not valid JSON: %s", source))
		}
		raw = append(bytes.TrimSpace(raw), '\n')
		if err := os.WriteFile(destination, raw, 0o644); err != nil {
			panic(fmt.Errorf("write generated bank file %s: %w", id, err))
		}
		fmt.Printf("synced %s -> %s\n", source, destination)
	}
}
