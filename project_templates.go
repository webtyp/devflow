package devflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ValidateRepoName validates the repository name
// Only alphanumeric, dash, and underscore allowed
func ValidateRepoName(name string) error {
	if name == "" {
		return fmt.Errorf("repository name is required")
	}
	matched, err := regexp.MatchString(`^[a-zA-Z0-9-_]+$`, name)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid repository name: only alphanumeric, dash, and underscore allowed")
	}
	return nil
}

// ValidateDescription validates the repository description
func ValidateDescription(desc string) error {
	if desc == "" {
		return fmt.Errorf("description is required")
	}
	if len(desc) > 350 {
		return fmt.Errorf("description too long (max 350 chars)")
	}
	return nil
}

// GenerateREADME generates README.md
func GenerateREADME(repoName, description, targetDir string) error {
	content := fmt.Sprintf("# %s\n\n%s\n", repoName, description)
	return os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(content), 0644)
}

// GenerateLicense generates LICENSE (MIT)
func GenerateLicense(ownerName, targetDir string) error {
	year := time.Now().Year()
	content := fmt.Sprintf(`MIT License

Copyright (c) %d %s

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`, year, ownerName)
	return os.WriteFile(filepath.Join(targetDir, "LICENSE"), []byte(content), 0644)
}

// GenerateGitignore generates .gitignore for Go
func GenerateGitignore(targetDir string) error {
	content := `# Binaries for programs and plugins
.env
*.exe
*.exe~
*.dll
*.log

# Test binary, built with 'go test -c'
*.test

# Output of the go coverage tool, specifically when used with LiteIDE
coverage.html
*.out

# Dependency directories (remove the comment below to include it)
# vendor/

# Go workspace file
*.code-workspace
.vscode
go.work
`
	return os.WriteFile(filepath.Join(targetDir, ".gitignore"), []byte(content), 0644)
}

// GenerateHandlerFile generates the main handler file
func GenerateHandlerFile(repoName, targetDir string) error {
	// Convert repo-name to RepoName
	structName := KebabToCamel(repoName)
	// Convert RepoName to r (first letter lowercase)
	// Actually spec says: First letter lowercase of struct name
	// But it says: Variable: First letter lowercase of struct name (e.g., m := &MyRepo{})
	// This is for usage, but what about the file content?
	// "Basic struct + New() constructor (bash script pattern)"

	packageName := strings.ReplaceAll(repoName, "-", "")
	packageName = strings.ReplaceAll(packageName, "_", "")
	packageName = strings.ToLower(packageName)

	content := fmt.Sprintf(`package %s

type %s struct {}

func New() *%s {
    return &%s{}
}
`, packageName, structName, structName, structName)

	filename := fmt.Sprintf("%s.go", repoName)
	return os.WriteFile(filepath.Join(targetDir, filename), []byte(content), 0644)
}

// KebabToCamel converts kebab-case or snake_case to CamelCase
func KebabToCamel(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_'
	})

	var result strings.Builder
	for _, part := range parts {
		if len(part) > 0 {
			result.WriteString(strings.ToUpper(part[:1]))
			result.WriteString(part[1:])
		}
	}
	return result.String()
}
