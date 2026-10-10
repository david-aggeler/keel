// Package logsink holds the absent-logger rule shared by keel/exec and its
// claude and codex adapters.
package logsink

import "reflect"

// Absent reports whether logger supplies no sink: a nil interface, or a nil
// pointer (or other nil-able value) held inside a non-nil interface. A typed
// nil passes a plain "== nil" check and then panics on its first record, so
// every injection point tests with Absent instead.
//
// DHF-REQ: keel/requirement-122 (keel/ac-795)
func Absent(logger any) bool {
	if logger == nil {
		return true
	}
	v := reflect.ValueOf(logger)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	}
	return false
}
