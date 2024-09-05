package main

import (
	"fmt"
	"go/ast"
	"go/types"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/packages"
)

var clientMethodCallCount int
var resourceName string

// Function to append a value to a map, initializing the key if it doesn't exist
func addValueToMap(m map[string][]string, key, value string) {
	// Check if the key exists in the map
	if _, exists := m[key]; !exists {
		// If the key doesn't exist, initialize it with an empty slice
		m[key] = []string{}
	}
	// Append the new value to the slice for that key
	m[key] = append(m[key], value)
}

// removeDuplicatesFromSlice removes duplicate values from a slice of strings
func removeDuplicatesFromSlice(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, value := range slice {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// removeDuplicatesFromMap removes duplicates from each slice in the map
func removeDuplicatesFromMap(m map[string][]string) {
	for key, slice := range m {
		m[key] = removeDuplicatesFromSlice(slice)
	}
}

// Function to print the map in a readable format
func printMap(m map[string][]string) {
	for key, values := range m {
		fmt.Printf("%s: [", key)
		for i, value := range values {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(value)
		}
		fmt.Println("]")
	}
}

func find_client_method_call(
	pkgs []*packages.Package, methodName string, resourcePerListMethods map[string][]string,
) {
	targetPackage := "sigs.k8s.io/controller-runtime/pkg/client"

	// Process each package
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
						if sel.Sel.Name == methodName {
							pos := pkg.Fset.Position(call.Pos())
							// Find the package of the method
							if methodObj := pkg.TypesInfo.ObjectOf(sel.Sel); methodObj != nil {
								if methodObj.Pkg() != nil && methodObj.Pkg().Path() == targetPackage {
									// Print results only if the method is from the desired package
									fmt.Printf("Found %s in file: %s\n", methodName, pos.Filename)
									fmt.Printf("Line: %d, Column: %d\n", pos.Line, pos.Column)

									// Print the full selector expression
									fmt.Printf("Full expression: %s\n", types.ExprString(sel))

									// Print argument information with types and save the resource name

									fmt.Printf("Arguments:\n")
									for i, arg := range call.Args {
										argType := pkg.TypesInfo.Types[arg].Type
										fmt.Printf("  Arg %d: %s (Type: %s)\n", i+1, types.ExprString(arg), argType)
										resourceName = argType.String()
									}

									// Print the method's receiver type, if available
									if funcType, ok := methodObj.Type().(*types.Signature); ok {
										if recv := funcType.Recv(); recv != nil {
											fmt.Printf("Method of type: %s\n", recv.Type())
										}
									}
									fmt.Printf("Defined in package: %s\n", methodObj.Pkg().Path())
									fmt.Println("---")
									clientMethodCallCount++
									addValueToMap(resourcePerListMethods, resourceName, methodName)

								}
							}
						}
					}
				}
				return true
			})
		}
	}
	removeDuplicatesFromMap(resourcePerListMethods)
	fmt.Printf("Total method calls found: %d\n", clientMethodCallCount)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: ./go-ast-analyzer <path_to_go_repo>")
		os.Exit(1)
	}

	repoPath, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatalf("Error getting absolute path: %v", err)
	}

	// Change to the repository directory
	err = os.Chdir(repoPath)
	if err != nil {
		log.Fatalf("Error changing to repository directory: %v", err)
	}

	// Configure the loader
	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypes | packages.NeedImports,
		// You might need to adjust this if your project uses modules
		Dir: repoPath,
	}

	// Load the packages
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		log.Fatalf("Error loading packages: %v", err)
	}

	resourcePerListMethods := make(map[string][]string)
	method_names := []string{"Get", "Update", "Create", "Delete"}

	for _, method_name := range method_names {
		find_client_method_call(pkgs, method_name, resourcePerListMethods)
	}
	fmt.Println("Print the resources per list methods:")
	printMap(resourcePerListMethods)
}
