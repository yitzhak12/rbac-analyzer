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

const targetPackage = "sigs.k8s.io/controller-runtime/pkg/client"

var methodArgMap = map[string]int{
	"Get":    3,
	"Update": 2,
	"Create": 2,
	"Delete": 2,
	"Patch":  2,
}

type Analyzer struct {
	clientMethodCallCount  int
	resourcePerListMethods map[string]map[string]bool
}

func NewAnalyzer() *Analyzer {
	return &Analyzer{
		resourcePerListMethods: make(map[string]map[string]bool),
	}
}

func (a *Analyzer) addValueToMap(key, value string) {
	if _, exists := a.resourcePerListMethods[key]; !exists {
		a.resourcePerListMethods[key] = make(map[string]bool)
	}
	a.resourcePerListMethods[key][value] = true
}

func (a *Analyzer) printMap() {
	for key, valueSet := range a.resourcePerListMethods {
		values := make([]string, 0, len(valueSet))
		for value := range valueSet {
			values = append(values, value)
		}
		fmt.Printf("%s: %v\n", key, values)
	}
}

func (a *Analyzer) findClientMethodCalls(pkgs []*packages.Package) {
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
						methodName := sel.Sel.Name
						if argIndex, exists := methodArgMap[methodName]; exists {
							a.processMethodCall(pkg, call, sel, methodName, argIndex)
						}
					}
				}
				return true
			})
		}
	}
}

func (a *Analyzer) processMethodCall(pkg *packages.Package, call *ast.CallExpr, sel *ast.SelectorExpr, methodName string, argIndex int) {
	if methodObj := pkg.TypesInfo.ObjectOf(sel.Sel); methodObj != nil && methodObj.Pkg() != nil && methodObj.Pkg().Path() == targetPackage {
		pos := pkg.Fset.Position(call.Pos())
		fmt.Printf("Found %s in file: %s\n", methodName, pos.Filename)
		fmt.Printf("Line: %d, Column: %d\n", pos.Line, pos.Column)
		fmt.Printf("Full expression: %s\n", types.ExprString(sel))

		arg := call.Args[argIndex-1]
		argType := pkg.TypesInfo.Types[arg].Type
		fmt.Printf("  Arg %d: %s (Type: %s)\n", argIndex, types.ExprString(arg), argType)
		resourceName := argType.String()

		if funcType, ok := methodObj.Type().(*types.Signature); ok {
			if recv := funcType.Recv(); recv != nil {
				fmt.Printf("Method of type: %s\n", recv.Type())
			}
		}
		fmt.Printf("Defined in package: %s\n", methodObj.Pkg().Path())
		fmt.Println("---")

		a.clientMethodCallCount++
		a.addValueToMap(resourceName, methodName)
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: ./rbacanalyzer <path_to_go_repo>")
		os.Exit(1)
	}

	repoPath, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatalf("Error getting absolute path: %v", err)
	}

	if err := os.Chdir(repoPath); err != nil {
		log.Fatalf("Error changing to repository directory: %v", err)
	}

	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypes | packages.NeedImports,
		Dir:  repoPath,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		log.Fatalf("Error loading packages: %v", err)
	}

	analyzer := NewAnalyzer()
	analyzer.findClientMethodCalls(pkgs)

	fmt.Printf("Total method calls found: %d\n", analyzer.clientMethodCallCount)
	fmt.Println("Resources per list methods:")
	analyzer.printMap()
}
