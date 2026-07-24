// Package keel exposes the module-level version-of-record.
package keel

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

//go:embed VERSION
var versionFile string

// BuildMetadata can be stamped at build time with
// -ldflags "-X github.com/david-aggeler/keel.BuildMetadata=<metadata>".
var BuildMetadata string

// DHF-REQ: keel/requirement-110
func Semver() string {
	return strings.TrimSpace(versionFile)
}

// DHF-REQ: keel/requirement-110
func Version() string {
	base := Semver()
	metadata := buildMetadata()
	if metadata == "" {
		return base
	}
	return base + "+" + metadata
}

func buildMetadata() string {
	if metadata := cleanBuildMetadata(BuildMetadata); metadata != "" {
		return metadata
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return cleanBuildMetadata(setting.Value)
		}
	}
	return ""
}

func cleanBuildMetadata(metadata string) string {
	metadata = strings.TrimSpace(metadata)
	metadata = strings.TrimPrefix(metadata, "v")
	metadata = strings.ReplaceAll(metadata, "+", ".")
	metadata = strings.ReplaceAll(metadata, "/", ".")
	metadata = strings.ReplaceAll(metadata, " ", ".")
	return strings.Trim(metadata, ".-")
}
