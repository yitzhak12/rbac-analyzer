package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

	"golang.org/x/tools/go/packages"
)

// List of client methods to be randomly generated
var clientMethods = []string{"Get", "Create", "Update", "Delete", "Patch"}

func createTempGoFileInPackage(pkgPath, content string) (string, error) {
	// Create the necessary directory structure to simulate the package path
	err := os.MkdirAll(pkgPath, os.ModePerm)
	if err != nil {
		return "", err
	}

	// Create the Go file in the specified package directory
	file, err := os.CreateTemp(pkgPath, "*.go")
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = file.WriteString(content)
	return file.Name(), err
}

// Randomly generate method calls and keep track of counts
func generateRandomMethodCalls() (string, map[string]int) {
	rand.Seed(time.Now().UnixNano())
	numCalls := rand.Intn(20) + 1 // Generate between 1 and 20 method calls
	callCounts := make(map[string]int)
	goFileContent := `package client

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

type Client struct{}

func main() {
	var c Client
`

	// Randomly select and generate method calls
	for i := 0; i < numCalls; i++ {
		method := clientMethods[rand.Intn(len(clientMethods))]
		callCounts[method]++

		switch method {
		case "Get":
			goFileContent += "\tdeployment := &appsv1.Deployment{}\n"
			goFileContent += fmt.Sprintf("\tc.%s(context.TODO(), types.NamespacedName{Name: \"name\", Namespace: \"namespace\"}, deployment)\n", method)
		case "Create", "Update", "Delete", "Patch":
			goFileContent += fmt.Sprintf("\tc.%s(context.TODO(), &appsv1.Deployment{})\n", method)
		}
	}

	goFileContent += "}\n"
	return goFileContent, callCounts
}

func TestAnalyzerWithRandomCalls(t *testing.T) {
	// Create a temporary directory to simulate a Go workspace
	tempDir, err := os.MkdirTemp("", "analyzer_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up after the test

	// Add the necessary modules to the go.mod file
	err = os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(`
module sigs.k8s.io/controller-runtime

go 1.16

require (
	k8s.io/api v0.21.0
	k8s.io/apimachinery v0.21.0
)
`), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod file: %v", err)
	}

	// Define the simulated package path to match `sigs.k8s.io/controller-runtime/pkg/client`
	packagePath := filepath.Join(tempDir, "pkg", "client")

	// Ensure that the directory structure is created
	err = os.MkdirAll(packagePath, os.ModePerm)
	if err != nil {
		t.Fatalf("Failed to create package path: %v", err)
	}

	// Generate random method calls and track the expected counts
	goFileContent, expectedCallCounts := generateRandomMethodCalls()

	// Print the generated Go file content
	fmt.Println("Generated Go file content:\n", goFileContent)

	// Create the Go file with random method calls
	_, err = createTempGoFileInPackage(packagePath, goFileContent)
	if err != nil {
		t.Fatalf("Failed to create Go file: %v", err)
	}

	// Set up the logger with DEBUG level
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Load the packages using Go modules
	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypes | packages.NeedImports,
		Dir:  tempDir, // Base directory for the module
	}

	// Load the package and log package details
	pkgs, err := packages.Load(cfg, "sigs.k8s.io/controller-runtime/pkg/client")
	if err != nil {
		t.Fatalf("Failed to load packages: %v", err)
	}

	// Initialize the analyzer
	analyzer := NewAnalyzer(logger)
	analyzer.findClientMethodCalls(pkgs)

	// Print the contents of the analyzer for debugging
	fmt.Println("Analyzer results:")
	for resource, methods := range analyzer.resourcePerListMethods {
		fmt.Printf("Resource: %s, Methods: %v\n", resource, methods)
	}

	// Print the entire structure of analyzer.resourcePerListMethods for deeper debugging
	fmt.Printf("\nFull analyzer.resourcePerListMethods structure: %+v\n\n", analyzer.resourcePerListMethods)

	// Verify that the method counts match
	for method, expectedCount := range expectedCallCounts {
		fmt.Printf("Checking method %s: expected %d calls\n", method, expectedCount)

		if resourceMethods, exists := analyzer.resourcePerListMethods["*k8s.io/api/apps/v1.Deployment"]; exists {
			actualCount := 0
			if resourceMethods[method] {
				actualCount = len(resourceMethods)
			}
			if actualCount != expectedCount {
				t.Errorf("Expected %d calls to %s, but got %d", expectedCount, method, actualCount)
			}
		} else {
			t.Errorf("No calls to method %s found", method)
		}
	}
}
