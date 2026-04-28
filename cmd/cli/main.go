package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/carbon-os/arc/packaging"
)

// ArcConfig is the wrapper for the project's configuration file.
type ArcConfig struct {
	AppName string                    `json:"appName"`
	Package packaging.PackagingConfig `json:"package"`
}

func main() {
	// Define CLI flags
	configFlag := flag.String("config", "arc.json", "Path to the configuration file")
	targetFlag := flag.String("target", "", "Comma-separated targets (macos, windows, linux)")
	skipSign := flag.Bool("skip-sign", false, "Skip code signing")
	flag.Parse()

	// 1. Read the configuration file using the custom path
	data, err := os.ReadFile(*configFlag)
	if err != nil {
		fmt.Printf("Fatal: Could not read config file at '%s': %v\n", *configFlag, err)
		os.Exit(1)
	}

	// 2. Parse the config
	var arcCfg ArcConfig
	if err := json.Unmarshal(data, &arcCfg); err != nil {
		fmt.Printf("Fatal: Invalid JSON format in '%s': %v\n", *configFlag, err)
		os.Exit(1)
	}

	// 3. Parse targets
	var targets []string
	if *targetFlag != "" {
		targets = strings.Split(*targetFlag, ",")
	}

	// 4. Delegate to the packaging pipeline
	opts := packaging.BuildOptions{SkipSign: *skipSign}

	fmt.Printf("Building %s (Config: %s)...\n", arcCfg.AppName, *configFlag)
	if err := packaging.Build(arcCfg.Package, arcCfg.AppName, targets, opts); err != nil {
		fmt.Printf("\n❌ Build failed: %v\n", err)
		os.Exit(1)
	}
}