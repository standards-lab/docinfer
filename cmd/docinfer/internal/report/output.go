package report

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
)

// Make renders the report of the run id in docinfer's directory dir, as [Render] does, then
// prints its index page's path to w, absolute, on a line of its own.
func Make(ctx context.Context, w io.Writer, dir, id string) error {
	if err := Render(ctx, dir, id); err != nil {
		return err
	}
	d, err := outdir.Open(dir)
	if err != nil {
		return err
	}
	rel, err := d.ReportDir(id)
	if err != nil {
		return err
	}
	index, err := filepath.Abs(filepath.Join(dir, filepath.FromSlash(rel), IndexPage))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, index)
	return err
}
