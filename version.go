// Package keel exposes the module-level version-of-record.
package keel

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// BuildNumber carries the monotonic build number — the commit count since the
// repository's first commit — stamped at build time with
// -ldflags "-X github.com/david-aggeler/keel.BuildNumber=<n>".
// Package keel never executes git; an unstamped build leaves it empty.
var BuildNumber string

// DHF-REQ: keel/requirement-110
func Semver() string {
	return strings.TrimSpace(versionFile)
}

// Version returns the dotted MAJOR.MINOR.PATCH.BUILD form when a build number
// was stamped, and the bare VERSION semver otherwise. A stamp that is not a
// plain digit string is ignored so the surface stays 4-part numeric.
//
// DHF-REQ: keel/requirement-110 (keel/ac-688, keel/ac-689)
func Version() string {
	base := Semver()
	if isDigits(BuildNumber) {
		return base + "." + BuildNumber
	}
	return base
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
