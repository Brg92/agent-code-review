package guards

import (
	"fmt"
	"strconv"
	"strings"
)

// ExitCode is a row of the table in docs/exit-codes.md.
type ExitCode struct {
	Code int
	// Status is Binding or Reserved.
	Status string
	// All is true when the commands cell says every command.
	All bool
	// Commands are the commands that return the code, when All is false.
	Commands []string
}

// ParseExitCodes reads the table of docs/exit-codes.md: a header with the
// columns Code, Status, Meaning, Commands and Typical cause, then a row per
// code. The Commands cell is "all", "none", or backticked command names
// separated by commas; a binding code names at least one command.
func ParseExitCodes(text string) ([]ExitCode, error) {
	var rows []ExitCode
	seen := map[int]bool{}
	inTable := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			if inTable {
				break
			}
			continue
		}
		if !inTable {
			inTable = true
			continue // the header
		}
		if strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 5 {
			return nil, fmt.Errorf("a row has %d cells, want at least 5: %q", len(cells), line)
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		code, err := strconv.Atoi(cells[0])
		if err != nil || code < 0 {
			return nil, fmt.Errorf("%q is not an exit code", cells[0])
		}
		if seen[code] {
			return nil, fmt.Errorf("exit code %d is listed twice", code)
		}
		seen[code] = true
		row := ExitCode{Code: code, Status: cells[1]}
		if row.Status != "Binding" && row.Status != "Reserved" {
			return nil, fmt.Errorf("exit code %d has the status %q, want Binding or Reserved", code, row.Status)
		}
		switch cell := cells[3]; {
		case cell == "all":
			row.All = true
		case cell == "none":
		case cell == "":
			return nil, fmt.Errorf("exit code %d names no command", code)
		default:
			for _, c := range strings.Split(cell, ",") {
				name, ok := strings.CutSuffix(strings.TrimPrefix(strings.TrimSpace(c), "`"), "`")
				if !ok || strings.ContainsAny(name, " `") || name == "" {
					return nil, fmt.Errorf("exit code %d: command %q is not a backticked name", code, strings.TrimSpace(c))
				}
				row.Commands = append(row.Commands, name)
			}
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no table of exit codes")
	}
	return rows, nil
}
