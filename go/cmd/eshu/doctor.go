package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Println("Eshu Diagnostics")
	fmt.Println(strings.Repeat("-", 40))

	// Check config directory.
	dir := appHome()
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		fmt.Printf("  [ok] Config directory exists: %s\n", dir)
	} else {
		fmt.Printf("  [!!] Config directory missing: %s\n", dir)
	}

	// Check env file.
	envFile := envFilePath()
	if _, err := os.Stat(envFile); err == nil {
		fmt.Printf("  [ok] Config file exists: %s\n", envFile)
	} else {
		fmt.Printf("  [!!] Config file missing: %s\n", envFile)
	}

	// Check Go binaries.
	for _, bin := range []string{"eshu-api", "eshu-mcp-server", "eshu-bootstrap-index", "eshu-ingester", "eshu-reducer"} {
		if path, err := exec.LookPath(bin); err == nil {
			fmt.Printf("  [ok] %s found: %s\n", bin, path)
		} else {
			fmt.Printf("  [!!] %s not found in PATH\n", bin)
		}
	}

	// Check API connectivity.
	client := NewAPIClient("", "", "")
	healthClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := healthClient.Get(client.BaseURL + "/health")
	if err != nil {
		fmt.Printf("  [!!] API not reachable at %s\n", client.BaseURL)
	} else {
		_ = resp.Body.Close()
		if resp.StatusCode == 200 {
			fmt.Printf("  [ok] API healthy at %s\n", client.BaseURL)
		} else {
			fmt.Printf("  [!!] API returned status %d at %s\n", resp.StatusCode, client.BaseURL)
		}
	}

	// Check Neo4j.
	neo4jURI := os.Getenv("NEO4J_URI")
	if neo4jURI == "" {
		neo4jURI = resolveConfigValue("NEO4J_URI", "")
	}
	if neo4jURI != "" {
		fmt.Printf("  [ok] Neo4j URI configured: %s\n", neo4jURI)
	} else {
		fmt.Printf("  [!!] Neo4j URI not configured (set NEO4J_URI)\n")
	}

	// Check Postgres.
	pgDSN := os.Getenv("ESHU_POSTGRES_DSN")
	if pgDSN != "" {
		fmt.Printf("  [ok] Postgres DSN configured\n")
	} else {
		fmt.Printf("  [!!] Postgres DSN not configured (set ESHU_POSTGRES_DSN)\n")
	}

	return nil
}
