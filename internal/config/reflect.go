package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
)

// RedactedValue replaces non-empty secret values.
const RedactedValue = "***"

// Redacted returns a copy of the config with every secret:"true" string field
// masked.
func (c *Config) Redacted() *Config {
	cp := *c
	redactValue(reflect.ValueOf(&cp).Elem())
	return &cp
}

// RedactStruct masks secret fields of the struct pointed to by p, in place.
func RedactStruct(p any) {
	redactValue(reflect.ValueOf(p).Elem())
}

func redactValue(v reflect.Value) {
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if t.Field(i).Tag.Get("secret") == "true" && f.Kind() == reflect.String {
			if f.String() != "" {
				f.SetString(RedactedValue)
			}
			continue
		}
		redactValue(f)
	}
}

func yamlName(f reflect.StructField) string {
	return strings.Split(f.Tag.Get("yaml"), ",")[0]
}

func fieldTypes(c *Config) reflect.Type { return reflect.TypeOf(*c) }

func unknownKeys(raw map[string]any, prefix string, t reflect.Type) []string {
	var out []string
	for k, v := range raw {
		var field *reflect.StructField
		for i := 0; i < t.NumField(); i++ {
			if f := t.Field(i); yamlName(f) == k {
				field = &f
				break
			}
		}
		path := prefix + k
		if field == nil {
			out = append(out, fmt.Sprintf("unknown config key %q ignored", path))
			continue
		}
		if sub, ok := v.(map[string]any); ok && field.Type.Kind() == reflect.Struct {
			out = append(out, unknownKeys(sub, path+".", field.Type)...)
		}
	}
	return out
}

// Set changes the value at a dotted key (for example "ollama.url") and
// validates the result. List values are comma-separated. The config is not
// modified if the new value is invalid.
func (c *Config) Set(key, value string) error {
	cp := *c
	v := reflect.ValueOf(&cp).Elem()
	for _, part := range strings.Split(key, ".") {
		if v.Kind() != reflect.Struct {
			return domain.New(domain.ErrInvalid, fmt.Sprintf("unknown config key %q", key))
		}
		found := false
		for i := 0; i < v.NumField(); i++ {
			if yamlName(v.Type().Field(i)) == part {
				v = v.Field(i)
				found = true
				break
			}
		}
		if !found {
			return domain.New(domain.ErrInvalid, fmt.Sprintf("unknown config key %q", key))
		}
	}
	if err := setScalar(v, value); err != nil {
		return domain.New(domain.ErrInvalid, fmt.Sprintf("%s: %v", key, err))
	}
	if err := cp.Validate(); err != nil {
		return err
	}
	*c = cp
	return nil
}

func setScalar(v reflect.Value, s string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Int:
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("%q is not an integer", s)
		}
		v.SetInt(int64(n))
	case reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("%q is not a number", s)
		}
		v.SetFloat(f)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("%q is not true or false", s)
		}
		v.SetBool(b)
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported list type")
		}
		var items []string
		if s != "" {
			for _, p := range strings.Split(s, ",") {
				items = append(items, strings.TrimSpace(p))
			}
		}
		v.Set(reflect.ValueOf(items))
	case reflect.Struct:
		return fmt.Errorf("is a section; set one of its keys instead")
	default:
		return fmt.Errorf("unsupported type %s", v.Kind())
	}
	return nil
}
