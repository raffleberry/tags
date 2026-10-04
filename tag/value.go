package tag

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Tag is a multi-valued set of metadata fields keyed by lowercase names.
// Single-valued format fields use a one element slice. All accessors return
// multiple values.
type Tag map[string][]string

// New returns a Tag holding the given fields, in order.
func New(pairs ...string) Tag {
	t := Tag{}
	for i := 0; i+1 < len(pairs); i += 2 {
		t.Add(pairs[i], pairs[i+1])
	}
	return t
}

// Value returns the first non-empty value under key. It returns "" when the
// key is absent or all values are empty.
func (t Tag) Value(key string) string {
	for _, v := range t[key] {
		if v != "" {
			return v
		}
	}
	return ""
}

// Values returns a copy of the values stored under key, which may be empty.
// The result is never nil for a present key.
func (t Tag) Values(key string) []string {
	return slices.Clone(t[key])
}

// Int returns the first value stored under key parsed as a base 10 integer.
func (t Tag) Int(key string) (int, bool) {
	v := t.Value(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	return n, true
}

// Bool reports whether key is "1", "true" or "yes". Comparison ignores case
// and surrounding space.
func (t Tag) Bool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(t.Value(key))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// Add appends values under key, keeping any values already present.
func (t Tag) Add(key string, values ...string) {
	t[key] = append(t[key], values...)
}

// Set replaces any values stored under key.
func (t Tag) Set(key string, values ...string) {
	if len(values) == 0 {
		delete(t, key)
		return
	}
	t[key] = slices.Clone(values)
}

// SetDefault stores value under key when the key has no non-empty value. It
// reports whether the value was stored. Format readers use it to add data from
// a secondary source. Example: ID3v1 data when ID3v2 data is present.
func (t Tag) SetDefault(key, value string) bool {
	if t.Value(key) != "" {
		return false
	}
	t.Set(key, value)
	return true
}

// Delete removes key and its values.
func (t Tag) Delete(key string) { delete(t, key) }

// Keys returns the keys present in t in sorted order.
func (t Tag) Keys() []string { return slices.Sorted(maps.Keys(t)) }

// Clone returns a deep copy of t, so the result can be modified freely.
func (t Tag) Clone() Tag {
	c := make(Tag, len(t))
	for key, values := range t {
		c[key] = slices.Clone(values)
	}
	return c
}

// Bool formats b per the ID3v2.3 specification, as "1" or "0".
func Bool(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
