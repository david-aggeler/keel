package vscode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const ImportMaxLineBytes = 4 * 1024 * 1024

// ImportReport summarizes an imported external run stream.
type ImportReport struct {
	ExitCode       int
	Events         int
	TruncatedLines int
	Warnings       []string
}

// ImportExternalRun normalizes an external producer's run-event JSONL stream,
// sanitizes hostile artifact URIs, and keeps truncated or invalid input
// visible as output/warning events instead of silently dropping it.
//
// DHF-REQ: keel/requirement-23
func ImportExternalRun(workspace string, r io.Reader, producerErr error, emit RunEventWriter, logf func(string)) ImportReport {
	var normalized bytes.Buffer
	report := ImportReport{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), ImportMaxLineBytes)
	for scanner.Scan() {
		normalized.Write(scanner.Bytes())
		normalized.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			report.TruncatedLines++
			report.Warnings = append(report.Warnings, "external run line exceeded 4 MiB and was truncated")
			normalized.WriteString(`{"event":"output","message":"external run line exceeded 4 MiB and was truncated"}` + "\n")
		} else if producerErr == nil {
			producerErr = err
		}
	}

	wrapped := func(event RunEvent) {
		if event.Artifact != nil {
			artifact, ok, warning := sanitizeArtifactURI(workspace, event.Artifact)
			if !ok {
				report.Warnings = append(report.Warnings, warning)
				if logf != nil {
					logf(warning)
				}
				emit(RunEvent{Event: "output", TestID: event.TestID, Message: warning})
				report.Events++
				return
			}
			event.Artifact = &artifact
		}
		emit(event)
		report.Events++
	}
	report.ExitCode = NormalizeRunEvents(&normalized, producerErr, wrapped, logf)
	return report
}

// MarshalRunEventJSONL encodes a run event as one compact JSONL record.
func MarshalRunEventJSONL(event RunEvent) ([]byte, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("keel/vscode: marshal run event: %w", err)
	}
	return append(body, '\n'), nil
}
