package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func scanKeelDevConfigDocumentation(root string, files []string) ([]string, error) {
	if !listedLintFile(files, filepath.Join("cmd", "keel-dev", "config.go")) {
		return nil, nil
	}
	var violations []string
	v, err := scanConfigStructFieldDocs(root, files)
	if err != nil {
		return nil, err
	}
	violations = append(violations, v...)
	configFile := filepath.Join(root, keelDevConfigFile)
	if _, err := os.Stat(configFile); errors.Is(err, os.ErrNotExist) {
		violations = append(violations, fmt.Sprintf("  keel-dev-config-docs: %s is missing (keel/ac-450)", configFile))
	} else if err != nil {
		return nil, err
	} else {
		v, err = scanConfigFileKeyComments(configFile)
		if err != nil {
			return nil, err
		}
		violations = append(violations, v...)
	}
	sort.Strings(violations)
	return violations, nil
}

func scanConfigStructFieldDocs(root string, files []string) ([]string, error) {
	var violations []string
	err := visitGoFiles(root, filesWithPrefix(files, filepath.Join("cmd", "keel-dev")), func(path string, file *ast.File, fset *token.FileSet) {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					if yamlTag(field) == "" {
						continue
					}
					comment := strings.TrimSpace(fieldComment(field))
					if comment == "" {
						for _, name := range field.Names {
							pos := fset.Position(field.Pos())
							violations = append(violations, fmt.Sprintf("  keel-dev-config-docs: %s:%d field %s.%s has no config doc comment (keel/ac-450)", path, pos.Line, ts.Name.Name, name.Name))
						}
					}
				}
			}
		}
	})
	return violations, err
}

func listedLintFile(files []string, want string) bool {
	want = filepath.ToSlash(want)
	for _, file := range files {
		if filepath.ToSlash(file) == want {
			return true
		}
	}
	return false
}

func yamlTag(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag := field.Tag.Value
	if i := strings.Index(tag, `yaml:"`); i >= 0 {
		rest := tag[i+len(`yaml:"`):]
		if j := strings.Index(rest, `"`); j >= 0 {
			name, _, _ := strings.Cut(rest[:j], ",")
			return name
		}
	}
	return ""
}

func fieldComment(field *ast.Field) string {
	switch {
	case field.Doc != nil:
		return field.Doc.Text()
	case field.Comment != nil:
		return field.Comment.Text()
	default:
		return ""
	}
}

func scanConfigFileKeyComments(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var violations []string
	var comments []string
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			comments = nil
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			comments = append(comments, strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
			continue
		}
		if !isYAMLKeyLine(trimmed) {
			continue
		}
		key := strings.TrimSpace(strings.TrimSuffix(trimmed, ":"))
		key = strings.TrimPrefix(key, "- ")
		key, _, _ = strings.Cut(key, ":")
		comment := strings.TrimSpace(strings.Join(comments, " "))
		if len(comment) < 32 || !strings.Contains(comment, " ") {
			violations = append(violations, fmt.Sprintf("  keel-dev-config-docs: %s:%d key %s lacks a substantive preceding comment (keel/ac-450)", file, i+1, key))
		}
		comments = nil
	}
	return violations, nil
}

func isYAMLKeyLine(trimmed string) bool {
	if strings.HasPrefix(trimmed, "- ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	}
	if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, ":") {
		return false
	}
	key, _, _ := strings.Cut(trimmed, ":")
	if key == "" || strings.ContainsAny(key, " \t{}[]") {
		return false
	}
	return true
}
