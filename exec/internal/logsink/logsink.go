// Package logsink holds the absent-value rule shared by keel/exec and its
// claude and codex adapters.
package logsink

import "reflect"

// Absent reports whether value supplies nothing: a nil interface, or a nil
// pointer (or other nil-able value) held inside a non-nil interface. A typed
// nil passes a plain "== nil" check and then panics on its first method call,
// so every optional interface field of exec.Request (Logger, FailureLevel)
// tests with Absent instead.
//
// DHF-REQ: keel/requirement-122 (keel/ac-795), keel/requirement-24 (keel/ac-799)
func Absent(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	}
	return false
}
