package vscode

import (
	"strings"
	"testing"
)

// TestItemDescriptionRefusesTypedFactReEncodedAsProse holds keel/ac-552: a
// producer that duplicates a typed fact as a key=value token inside the prose
// channel is refused at the boundary, and the diagnostic names both the token
// it found and the typed field that should have carried the fact.
//
// DHF-TEST: keel/requirement-138
func TestItemDescriptionRefusesTypedFactReEncodedAsProse(t *testing.T) {
	for _, tc := range []struct {
		name           string
		item           TestItem
		wantMarker     string
		wantTypedField string
	}{
		{
			name:           "mutually_exclusive",
			item:           TestItem{ID: "keel::group::data-set", Description: "mutually_exclusive=true"},
			wantMarker:     "mutually_exclusive=",
			wantTypedField: "desired_state_group.mutually_exclusive",
		},
		{
			name:           "current",
			item:           TestItem{ID: "keel::row::app-db", Description: "current=empty"},
			wantMarker:     "current=",
			wantTypedField: "desired_state_row.current",
		},
		{
			name:           "action",
			item:           TestItem{ID: "keel::row::app-db", Description: "action=reconcile_during_run"},
			wantMarker:     "action=",
			wantTypedField: "desired_state_row.action",
		},
		{
			name:           "active",
			item:           TestItem{ID: "keel::row::app-db", Description: "seeded fixture; active=false"},
			wantMarker:     "active=",
			wantTypedField: "desired_state_row.active",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateItemDescription(tc.item)
			if err == nil {
				t.Fatalf("ValidateItemDescription(%q) = nil, want a refusal", tc.item.Description)
			}
			if !strings.Contains(err.Error(), tc.wantMarker) {
				t.Errorf("diagnostic %q does not name the offending token %q", err, tc.wantMarker)
			}
			if !strings.Contains(err.Error(), tc.wantTypedField) {
				t.Errorf("diagnostic %q does not name the typed field %q", err, tc.wantTypedField)
			}
			if !strings.Contains(err.Error(), tc.item.ID) {
				t.Errorf("diagnostic %q does not name the offending item %q", err, tc.item.ID)
			}
		})
	}
}

// TestItemDescriptionAcceptsProse holds the other half of keel/ac-552: the
// check refuses a re-encoded fact, not prose that merely reads like one.
//
// DHF-TEST: keel/requirement-138
func TestItemDescriptionAcceptsProse(t *testing.T) {
	for _, description := range []string{
		"",
		"the keel/log package",
		"reconciled during the run when the current data set is active",
		"· last 9.8s",
	} {
		if err := ValidateItemDescription(TestItem{ID: "keel::lane::go-log", Description: description}); err != nil {
			t.Errorf("ValidateItemDescription(%q) = %v, want nil", description, err)
		}
	}
}
