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
// visible as output/warning events instead of silently dropping it. An
// over-cap line is accounted and replaced by a warning record while the
// records after it keep importing — one hostile line never discards the
// rest of the stream.
//
// DHF-REQ: keel/requirement-23
func ImportExternalRun(workspace string, r io.Reader, producerErr error, emit RunEventWriter, logf func(string)) ImportReport {
	var normalized bytes.Buffer
	report := ImportReport{}
	reader := bufio.NewReaderSize(r, 64*1024)
	for {
		line, tooLong, err := readCappedLine(reader, ImportMaxLineBytes)
		switch {
		case tooLong:
			report.TruncatedLines++
			report.Warnings = append(report.Warnings, "external run line exceeded 4 MiB and was truncated")
			normalized.WriteString(`{"event":"output","message":"external run line exceeded 4 MiB and was truncated"}` + "\n")
		case err == nil || len(line) > 0:
			normalized.Write(line)
			normalized.WriteByte('\n')
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && producerErr == nil {
				producerErr = err
			}
			break
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

// readCappedLine reads one newline-terminated line from reader, accumulating
// at most maxBytes bytes. A longer line is discarded through its newline and
// reported as tooLong, leaving the reader positioned at the next line. err is
// io.EOF once the stream is exhausted; a final unterminated line is returned
// alongside it.
func readCappedLine(reader *bufio.Reader, maxBytes int) (line []byte, tooLong bool, err error) {
	for {
		chunk, readErr := reader.ReadSlice('\n')
		hadNewline := readErr == nil
		if hadNewline {
			chunk = chunk[:len(chunk)-1]
		}
		if !tooLong {
			if len(line)+len(chunk) > maxBytes {
				tooLong = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		switch {
		case hadNewline:
			return line, tooLong, nil
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		default:
			return line, tooLong, readErr
		}
	}
}

// MarshalRunEventJSONL encodes a run event as one compact JSONL record.
func MarshalRunEventJSONL(event RunEvent) ([]byte, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("keel/vscode: marshal run event: %w", err)
	}
	return append(body, '\n'), nil
}
