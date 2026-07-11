package vscode

import "testing"

func assertRunEvent(t *testing.T, events []RunEvent, eventName, testID string) RunEvent {
	t.Helper()
	for _, event := range events {
		if event.Event == eventName && event.TestID == testID {
			return event
		}
	}
	t.Fatalf("missing event %q test %q in %#v", eventName, testID, events)
	return RunEvent{}
}

func assertRunEventWithArtifactName(t *testing.T, events []RunEvent, testID, name string) RunEvent {
	t.Helper()
	for _, event := range events {
		if event.Event == "artifact" && event.TestID == testID && event.Artifact != nil && event.Artifact.Name == name {
			return event
		}
	}
	t.Fatalf("missing artifact %q test %q in %#v", name, testID, events)
	return RunEvent{}
}

func assertNoRunEvent(t *testing.T, events []RunEvent, eventName, testID string) {
	t.Helper()
	for _, event := range events {
		if event.Event == eventName && event.TestID == testID {
			t.Fatalf("unexpected event %q test %q in %#v", eventName, testID, events)
		}
	}
}
