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
	bankRoot := filepath.Clean(filepath.Join(
		workingDir,
		"..", "..", "..",
		"agent", "src", "hiddenworld", "bank",
	))
	syncBank(filepath.Join(bankRoot, "fixed"), filepath.Join(workingDir, "fixed_hiddenworld"), fixedBankIDs)
	syncBank(filepath.Join(bankRoot, "fixed_v3"), filepath.Join(workingDir, "fixed_scenario_v3"), fixedBankIDs)
	syncFile(
		filepath.Join(bankRoot, "fixed_v3", "manifest.json"),
		filepath.Join(workingDir, "fixed_scenario_v3", "manifest.json"),
	)
}

func syncBank(sourceDir, destinationDir string, ids []string) {
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		panic(fmt.Errorf("create generated bank directory %s: %w", destinationDir, err))
	}
	for _, id := range ids {
		syncFile(filepath.Join(sourceDir, id+".json"), filepath.Join(destinationDir, id+".json"))
	}
}

func syncFile(source, destination string) {
	raw, err := os.ReadFile(source)
	if err != nil {
		panic(fmt.Errorf("read source bank file %s: %w", source, err))
	}
	if !json.Valid(raw) {
		panic(fmt.Errorf("source bank file is not valid JSON: %s", source))
	}
	raw = append(bytes.TrimSpace(raw), '\n')
	if err := os.WriteFile(destination, raw, 0o644); err != nil {
		panic(fmt.Errorf("write generated bank file %s: %w", destination, err))
	}
	fmt.Printf("synced %s -> %s\n", source, destination)
}
