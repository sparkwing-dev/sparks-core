package release

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// VersionFileConfig locates a declared-version field in a manifest file and
// the tag naming that gates re-publishing it.
type VersionFileConfig struct {
	// Path is a manifest file relative to the repo root. Required.
	Path string
	// Field is the dotted key of the version, defaulting to "version" for
	// .json and "project.version" for .toml. Poetry needs
	// "tool.poetry.version".
	Field string
	// TagPrefix defaults to "v", so version "1.2.3" gates on tag "v1.2.3".
	TagPrefix string
}

// GuardVersionFile returns the version declared in a manifest file, erroring
// if its git tag already exists. It consults the local tag list, so on a
// tagless clone it passes even for an already-released version: fetch tags
// before relying on this gate.
func GuardVersionFile(ctx context.Context, cfg VersionFileConfig) (string, error) {
	if cfg.Path == "" {
		return "", fmt.Errorf("release: version file Path is required")
	}
	field := cfg.Field
	if field == "" {
		field = defaultVersionField(cfg.Path)
	}

	root := sparkwing.WorkDir()
	if root == "" {
		root = "."
	}
	data, err := os.ReadFile(filepath.Join(root, cfg.Path))
	if err != nil {
		return "", fmt.Errorf("release: read %s: %w", cfg.Path, err)
	}

	version, err := extractVersionField(cfg.Path, data, field)
	if err != nil {
		return "", err
	}
	if version == "" {
		return "", fmt.Errorf("release: %s has an empty %s", cfg.Path, field)
	}

	prefix := cfg.TagPrefix
	if prefix == "" {
		prefix = "v"
	}
	tag := prefix + version

	var guardErr error
	err = step.Run(ctx, "guard version ("+version+")", func(ctx context.Context) error {
		exists, terr := tagExists(ctx, tag)
		if terr != nil {
			return terr
		}
		if exists {
			guardErr = fmt.Errorf("release: %s declares version %s but tag %s already exists (bump the version to publish)", cfg.Path, version, tag)
			return guardErr
		}
		sparkwing.Info(ctx, "version %s is unreleased (no tag %s)", version, tag)
		return nil
	})
	if err != nil {
		return "", err
	}
	return version, nil
}

func defaultVersionField(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		return "project.version"
	default:
		return "version"
	}
}

func extractVersionField(path string, data []byte, field string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return extractJSONField(data, field)
	case ".toml":
		return extractTOMLField(data, field)
	default:
		return "", fmt.Errorf("release: unsupported version file %q (want .json or .toml)", path)
	}
}

func extractJSONField(data []byte, field string) (string, error) {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("release: parse JSON: %w", err)
	}
	cur := doc
	parts := strings.Split(field, ".")
	for i, key := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("release: field %q: %q is not an object", field, strings.Join(parts[:i], "."))
		}
		next, ok := obj[key]
		if !ok {
			return "", fmt.Errorf("release: field %q not found", field)
		}
		cur = next
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("release: field %q is not a string", field)
	}
	return s, nil
}

// extractTOMLField recognizes only the string-value form, which is all a
// version declaration uses.
func extractTOMLField(data []byte, field string) (string, error) {
	parts := strings.Split(field, ".")
	key := parts[len(parts)-1]
	wantTable := strings.Join(parts[:len(parts)-1], ".")

	curTable := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := stripTOMLComment(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			curTable = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if curTable != wantTable {
			continue
		}
		k, v, ok := splitTOMLKeyValue(line)
		if !ok || k != key {
			continue
		}
		return v, nil
	}
	return "", fmt.Errorf("release: field %q not found", field)
}

func stripTOMLComment(line string) string {
	inSingle, inDouble := false, false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(line[:i])
			}
		}
	}
	return line
}

func splitTOMLKeyValue(line string) (key, value string, ok bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	val := strings.TrimSpace(line[eq+1:])
	if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
		return key, val[1 : len(val)-1], true
	}
	return "", "", false
}

func tagExists(ctx context.Context, tag string) (bool, error) {
	out, err := sparkwing.Exec(ctx, "git", "tag", "--list", tag).String()
	if err != nil {
		return false, fmt.Errorf("release: git tag --list: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}
