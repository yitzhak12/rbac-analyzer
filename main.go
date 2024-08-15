package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type variableInfo struct {
	name string
	typ  string
}

func main() {
	repoPath := flag.String("repo", "", "Path to the repository")
	flag.Parse()

	if *repoPath == "" {
		fmt.Println("Please provide a repository path using -repo flag")
		return
	}

	err := filepath.Walk(*repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".go" && !strings.Contains(path, "vendor") && !strings.Contains(path, "_test.go") {
			if err := findGetCalls(path); err != nil {
				log.Printf("Error processing file %s: %v\n", path, err)
			}
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking the path %s: %v\n", *repoPath, err)
	}
}

func findGetCalls(filePath string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		return fmt.Errorf("failed to parse file: %v", err)
	}

	vars := make(map[string]variableInfo)

	// First pass: collect variable declarations
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok == token.VAR {
				for _, spec := range x.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						for i, name := range valueSpec.Names {
							if valueSpec.Type != nil {
								vars[name.Name] = variableInfo{name: name.Name, typ: exprToString(valueSpec.Type)}
							} else if i < len(valueSpec.Values) {
								vars[name.Name] = variableInfo{name: name.Name, typ: inferType(valueSpec.Values[i])}
							}
						}
					}
				}
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for i, lhs := range x.Lhs {
					if i < len(x.Rhs) {
						if ident, ok := lhs.(*ast.Ident); ok {
							vars[ident.Name] = variableInfo{name: ident.Name, typ: inferType(x.Rhs[i])}
						}
					}
				}
			}
		}
		return true
	})

	// Second pass: find r.Client.Get calls
	ast.Inspect(node, func(n ast.Node) bool {
		if callExpr, ok := n.(*ast.CallExpr); ok {
			if selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				if xIdent, ok := selectorExpr.X.(*ast.SelectorExpr); ok {
					if xIdent.Sel.Name == "Client" && selectorExpr.Sel.Name == "Get" {
						if len(callExpr.Args) >= 3 {
							instanceArg := callExpr.Args[2]
							fmt.Printf("Found r.Client.Get call in %s\n", filePath)
							fmt.Printf("  Instance argument: %s\n", exprToString(instanceArg))
							argType := inferType(instanceArg)
							if info, exists := vars[argType]; exists {
								argType = info.typ
							}
							fmt.Printf("  Inferred type: %s\n", argType)
							fmt.Printf("  At position: %s\n", fset.Position(callExpr.Pos()))
							fmt.Println()
						}
					}
				}
			}
		}
		return true
	})

	return nil
}

func inferType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e)
	case *ast.StarExpr:
		return "*" + inferType(e.X)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return "&" + inferType(e.X)
		}
	case *ast.CallExpr:
		return inferType(e.Fun) + "()"
	case *ast.CompositeLit:
		return exprToString(e.Type)
	}
	return fmt.Sprintf("%T", expr)
}

func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.CallExpr:
		return exprToString(e.Fun) + "()"
	case *ast.UnaryExpr:
		return e.Op.String() + exprToString(e.X)
	default:
		return fmt.Sprintf("%T", expr)
	}
}
