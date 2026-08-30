package cli

import "testing"

// TestCommandPath covers both branches of commandPath, the helper that names
// the node in a help diagnostic: the joined path when the topic resolved to a
// nested node, and the node's own name when the path is empty.
//
// DHF-TEST: keel/ac-640
func TestCommandPath(t *testing.T) {
	tests := []struct {
		name     string
		path     []string
		fallback string
		want     string
	}{
		{name: "empty path uses the fallback name", path: nil, fallback: "keel-dev", want: "keel-dev"},
		{name: "single element path", path: []string{"vsix"}, fallback: "keel-dev", want: "vsix"},
		{name: "nested path joins with spaces", path: []string{"vsix", "ci"}, fallback: "keel-dev", want: "vsix ci"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandPath(tc.path, tc.fallback); got != tc.want {
				t.Fatalf("commandPath(%q, %q) = %q, want %q", tc.path, tc.fallback, got, tc.want)
			}
		})
	}
}
