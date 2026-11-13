package hook

import (
	"encoding/json"

	"github.com/Rickedb/git-memory/internal/gm/core"
)

// Payload field names differ between the agents, and between the versions of one agent (snake_case vs camelCase), so
// every reader below accepts the spellings it has seen.

// pick returns the first non-null value among keys.
func pick(o any, keys ...string) any {
	m, ok := o.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range keys {
		if v := m[k]; v != nil {
			return v
		}
	}
	return nil
}

func pickStr(o any, keys ...string) string {
	s, _ := pick(o, keys...).(string)
	return s
}

// coalesce returns the first non-nil value.
func coalesce(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// asObject reads a tool payload part that may arrive as an object or as a JSON string.
func asObject(v any) core.Obj {
	switch x := v.(type) {
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(x), &parsed); err != nil {
			return core.Obj{"raw": x}
		}
		return asObject(parsed)
	case map[string]any:
		return x
	}
	return core.Obj{}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
