package conenv

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

func expandString(value string, vars map[string]string, secrets map[string]SecretRef) (string, error) {
	value = os.Expand(value, func(key string) string {
		if variable, ok := vars[key]; ok {
			return variable
		}
		return os.Getenv(key)
	})
	functions := template.FuncMap{
		"secret": func(name string) (string, error) {
			ref, ok := secrets[name]
			if !ok {
				return "", fmt.Errorf("unknown secret %q", name)
			}
			if ref.Env != "" {
				value, ok := os.LookupEnv(ref.Env)
				if !ok {
					return "", fmt.Errorf("secret %q references unset environment variable", name)
				}
				return value, nil
			}
			path, err := expandString(ref.File, vars, nil)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(data)), nil
		},
	}
	tmpl, err := template.New("value").Option("missingkey=error").Funcs(functions).Parse(value)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, vars); err != nil {
		return "", err
	}
	result := output.String()
	if strings.HasPrefix(result, "~/") || strings.HasPrefix(result, `~\`) {
		home := vars["home"]
		result = filepath.Join(home, result[2:])
	}
	return result, nil
}

func renderFile(path string, vars map[string]string, secrets map[string]SecretRef) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	sensitive := bytes.Contains(data, []byte("secret "))
	value, err := expandString(string(data), vars, secrets)
	return []byte(value), sensitive, err
}

func expandPath(value string, r *Resolved) (string, error) {
	// Paths are never allowed to interpolate secrets because paths appear in plans,
	// state, and error messages.
	value, err := expandString(value, r.Variables, nil)
	if err != nil {
		return "", err
	}
	return filepath.Clean(value), nil
}
