package run

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Adapted from spike-ai-pipeline (6a631f1): the table's error is joined with its flush's.
func writeModels(w io.Writer, check modelCheck) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var err error
	row := func(cells ...string) {
		if err == nil {
			_, err = io.WriteString(tw, strings.Join(cells, "\t")+"\n")
		}
	}
	row("CAPABILITY", "ENDPOINT", "MODEL", "STATE")
	for _, r := range check.rows {
		row(string(r.capability), r.endpoint, r.model, r.state)
	}
	if err := errors.Join(err, tw.Flush()); err != nil {
		return err
	}
	for _, warning := range check.warnings {
		if _, err := fmt.Fprintf(w, "warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}
