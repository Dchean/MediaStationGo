// Package handler — small utilities shared across handlers.
package handler

// toString converts a gin context value to a string, returning an empty
// string when the value is missing or of the wrong type.
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
