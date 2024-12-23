package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	logger                 *slog.Logger
	displayResourcePath    bool
}

func NewAnalyzer(logger *slog.Logger, displayResourcePath bool) *Analyzer {
	return &Analyzer{
		resourcePerListMethods: make(map[string]map[string]bool),
		logger:                 logger,
		displayResourcePath:    displayResourcePath,
	}
}

func (a *Analyzer) addValueToMap(key, value string) {
	if _, exists := a.resourcePerListMethods[key]; !exists {
		a.resourcePerListMethods[key] = make(map[string]bool)
	}
	a.resourcePerListMethods[key][value] = true
}

// extractResourceName isolates the type name from the full package path (e.g., "v1.StorageCluster" -> "StorageCluster")
func extractResourceName(fullName string) string {
	// Split by "." to get the type name
	parts := strings.Split(fullName, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1] // Return the name after the last dot
	}
	return fullName // Fallback to the original name if parsing fails
}

// getKubernetesResourceName converts CamelCase to Kubernetes-style names (e.g., StorageCluster to storagecluster)
func getKubernetesResourceName(s string) string {
	// Regular expression to find camel case boundaries
	var camelCasePattern = regexp.MustCompile("([a-z0-9])([A-Z])")
	// Convert camel case to lowercase and concatenate words
	resourceName := camelCasePattern.ReplaceAllString(s, "${1}${2}")
	return strings.ToLower(resourceName)
}

func (a *Analyzer) logMap() {
	for key, valueSet := range a.resourcePerListMethods {
		values := make([]string, 0, len(valueSet))
		for value := range valueSet {
			values = append(values, value)
		}
		// Use getKubernetesResourceName to get a more accurate Kubernetes resource name
		resourceName := extractResourceName(key)
		kubernetesResourceName := getKubernetesResourceName(resourceName)

		var output string
		if a.displayResourcePath {
			output = fmt.Sprintf(
				"Full Resource Name: %s\nResource: %s\nMethods: [%s]\n",
				key, kubernetesResourceName, strings.Join(values, ", "),
			)
		} else {
			output = fmt.Sprintf("Resource: %s\nMethods: [%s]\n", kubernetesResourceName, strings.Join(values, ", "))
		}
		fmt.Println(output)
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
		a.logger.Debug("Found method call",
			"method", methodName,
			"file", pos.Filename,
			"line", pos.Line,
			"column", pos.Column,
			"expression", types.ExprString(sel),
		)

		arg := call.Args[argIndex-1]
		argType := pkg.TypesInfo.Types[arg].Type
		a.logger.Debug("Argument info",
			"index", argIndex,
			"expression", types.ExprString(arg),
			"type", argType,
		)
		resourceName := argType.String()

		if funcType, ok := methodObj.Type().(*types.Signature); ok {
			if recv := funcType.Recv(); recv != nil {
				a.logger.Debug("Method receiver", "type", recv.Type())
			}
		}
		a.logger.Debug("Method definition", "package", methodObj.Pkg().Path())

		a.clientMethodCallCount++
		a.addValueToMap(resourceName, methodName)
	}
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	logLevel := flag.String("log-level", "INFO", "Set the logging level (DEBUG, INFO, WARN, ERROR)")
	displayResourcePath := flag.Bool("display-resource-path", false, "Display the full package path for the resource in log output")

	flag.Parse()

	if flag.NArg() != 1 {
		slog.Error("Invalid usage", "error", "Missing repository path")
		slog.Info("Usage: ./rbacanalyzer -log-level=<level> -display-resource-path=<true|false> <path_to_go_repo>")
		os.Exit(1)
	}

	level := parseLogLevel(*logLevel)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))

	repoPath, err := filepath.Abs(flag.Arg(0))
	if err != nil {
		logger.Error("Error getting absolute path", "error", err)
		os.Exit(1)
	}

	if err := os.Chdir(repoPath); err != nil {
		logger.Error("Error changing to repository directory", "error", err)
		os.Exit(1)
	}

	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypes | packages.NeedImports,
		Dir:  repoPath,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		logger.Error("Error loading packages", "error", err)
		os.Exit(1)
	}

	analyzer := NewAnalyzer(logger, *displayResourcePath)
	analyzer.findClientMethodCalls(pkgs)

	logger.Info("Analysis complete", "total method calls", analyzer.clientMethodCallCount)
	logger.Info("Resources per list methods:")
	analyzer.logMap()
}
