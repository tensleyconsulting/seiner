package keys

import (
	"reflect"
	"testing"

	"charm.land/bubbles/v2/key"
)

// TestDefaultKeysAreUnambiguous ensures no single keystroke is bound to two
// different bindings in Default(). A key may legitimately appear more than
// once within the same binding (e.g. "q" and "ctrl+c" both trigger Quit).
func TestDefaultKeysAreUnambiguous(t *testing.T) {
	t.Parallel()

	m := Default()
	v := reflect.ValueOf(m)
	typ := v.Type()

	owner := map[string]string{}
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		b, ok := field.Interface().(key.Binding)
		if !ok {
			t.Fatalf("field %s is not a key.Binding", typ.Field(i).Name)
		}
		name := typ.Field(i).Name
		for _, k := range b.Keys() {
			if existing, seen := owner[k]; seen && existing != name {
				t.Errorf("key %q is bound to both %s and %s", k, existing, name)
			}
			owner[k] = name
		}
	}

	if len(owner) == 0 {
		t.Fatal("Default() produced no key bindings")
	}
}
