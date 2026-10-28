package oyl

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// Dialect chooses how plain scalars are typed when decoding.
type Dialect int

const (
	// YAML12 types scalars by YAML 1.2's Core schema: true and false,
	// decimal, 0o and 0x integers, floats, null and ~. Everything else is
	// a string, yes, on and dates included. The default.
	YAML12 Dialect = iota
	// YAML11 types them as YAML 1.1 and PyYAML do: also yes/no/on/off,
	// 0-octal, 0b, base 60 and timestamps.
	YAML11
	// GoYAML2 types them as go-yaml v2 does, as Kubernetes reads YAML:
	// YAML 1.1's types and y/n; its merge order too.
	GoYAML2
)

func (d Dialect) String() string {
	switch d {
	case YAML11:
		return "YAML11"
	case GoYAML2:
		return "GoYAML2"
	}
	return "YAML12"
}

// An Option changes how Unmarshal and a Decoder read YAML.
type Option func(*options)

type options struct {
	dialect     Dialect
	knownFields bool
}

// WithDialect types plain scalars by d. A document that starts with
// %YAML 1.1 is read as YAML 1.1 whatever the dialect (as go-yaml v2 with
// GoYAML2).
func WithDialect(d Dialect) Option {
	return func(o *options) { o.dialect = d }
}

// WithKnownFields makes a mapping key that matches no field of the struct
// it is decoded into an error.
func WithKnownFields() Option {
	return func(o *options) { o.knownFields = true }
}

// Marshaler is a type that chooses what it is written as: the value
// MarshalYAML returns, written in its place.
type Marshaler interface {
	MarshalYAML() (any, error)
}

// Unmarshaler is a type that decodes itself: unmarshal decodes its YAML
// into any value, as Unmarshal would.
type Unmarshaler interface {
	UnmarshalYAML(unmarshal func(any) error) error
}

// A struct's fields as YAML keys.
type structInfo struct {
	fields    []fieldInfo
	byName    map[string]int
	inlineMap []int // the index of an ,inline map field, or nil
}

type fieldInfo struct {
	name      string
	index     []int
	omitEmpty bool
	flow      bool
}

var structInfos sync.Map // reflect.Type → *structInfo, or error

// getStructInfo reads a struct's fields as go-yaml does: the key is the
// yaml tag's name, else the json tag's (Kubernetes types have only
// those), else the field's name lowercased; "-" leaves a field out;
// options are omitempty, flow and inline (a struct's fields, or a map's
// pairs, as the parent's).
func getStructInfo(t reflect.Type) (*structInfo, error) {
	if v, ok := structInfos.Load(t); ok {
		if err, ok := v.(error); ok {
			return nil, err
		}
		return v.(*structInfo), nil
	}
	info := &structInfo{byName: map[string]int{}}
	err := info.add(t, nil, map[reflect.Type]bool{})
	if err != nil {
		structInfos.Store(t, err)
		return nil, err
	}
	structInfos.Store(t, info)
	return info, nil
}

func (info *structInfo) add(t reflect.Type, prefix []int, seen map[reflect.Type]bool) error {
	if seen[t] {
		return fmt.Errorf("yaml: %v is inlined into itself", t)
	}
	seen[t] = true
	defer delete(seen, t)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue // unexported
		}
		tag, ok := f.Tag.Lookup("yaml")
		if !ok {
			tag = f.Tag.Get("json")
		}
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		field := fieldInfo{name: name, index: append(append([]int{}, prefix...), i)}
		inline := false
		for _, opt := range strings.Split(opts, ",") {
			switch opt {
			case "omitempty":
				field.omitEmpty = true
			case "flow":
				field.flow = true
			case "inline":
				inline = true
			}
		}
		if inline {
			ft := f.Type
			if ft.Kind() == reflect.Pointer && ft.Elem().Kind() == reflect.Struct {
				ft = ft.Elem()
			}
			switch {
			case ft.Kind() == reflect.Struct:
				if err := info.add(ft, field.index, seen); err != nil {
					return err
				}
			case ft.Kind() == reflect.Map && ft.Key().Kind() == reflect.String:
				if info.inlineMap != nil {
					return fmt.Errorf("yaml: %v has two ,inline maps", t)
				}
				info.inlineMap = field.index
			default:
				return fmt.Errorf("yaml: %v.%s: ,inline needs a struct or a map with string keys", t, f.Name)
			}
			continue
		}
		if f.PkgPath != "" {
			continue // an unexported embedded type, not inlined
		}
		if field.name == "" {
			field.name = strings.ToLower(f.Name)
		}
		if _, dup := info.byName[field.name]; dup {
			return fmt.Errorf("yaml: %v has two fields for the key %q", t, field.name)
		}
		info.byName[field.name] = len(info.fields)
		info.fields = append(info.fields, field)
	}
	return nil
}
