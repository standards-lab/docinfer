package docinfer

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
)

type row int

const (
	rowRefused row = iota
	rowPermanent
	rowTransient
	rowFailed
)

func classify(err error) row {
	if apiErr, ok := errors.AsType[*ai.APIError](err); ok {
		return statusRow(apiErr.Status)
	}
	if _, ok := errors.AsType[*format.UnsupportedError](err); ok {
		return rowPermanent
	}
	if _, ok := errors.AsType[*jsontext.SyntacticError](err); ok {
		return rowFailed
	}
	if _, ok := errors.AsType[*json.SemanticError](err); ok {
		return rowFailed
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		return rowTransient
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return rowTransient
	}
	return rowFailed
}

func statusRow(status int) row {
	switch status {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity:
		return rowRefused
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return rowTransient
	case http.StatusInternalServerError:
		return rowFailed
	case http.StatusNotImplemented, http.StatusHTTPVersionNotSupported:
		return rowPermanent
	}
	switch {
	case status >= 400 && status < 500:
		return rowPermanent
	case status >= 500 && status < 600:
		return rowTransient
	}
	return rowFailed
}

func failureText(err error) string {
	if apiErr, ok := errors.AsType[*ai.APIError](err); ok {
		return apiErr.Error()
	}
	return err.Error()
}

type chunkError struct{ err error }

func (e *chunkError) Error() string { return e.err.Error() }
func (e *chunkError) Unwrap() error { return e.err }

type dimensionsError struct {
	digest    string
	got, want int
	held      bool
}

func (e *dimensionsError) Error() string {
	from := "the model made"
	if e.held {
		from = "the cache holds"
	}
	return fmt.Sprintf("docinfer: the vector %s for chunk %s has %d dimensions, want Config.Dimensions %d",
		from, e.digest, e.got, e.want)
}

// Permanent reports whether err, an error [Pipeline.Run] yields, is one no redelivery changes, so
// a host fails the job rather than redeliver it: a go-ai failure the failure table marks
// permanent (an *ai.APIError of 401, 403, 404, 501, 505, or any other 4xx but 408, 429, and the
// refused statuses, which are outcomes, not errors; or a *format.UnsupportedError), a mismatched
// input (dispatch.ErrSHA256), a dispatch's refused request (dispatch.ErrRequest: an input setting
// neither or both of Path and Reader, an input SHA-256 that isn't lowercase hex, or a resume
// cursor, Input.After, that names no message of the input's log), an input's defect (an image
// output without a MediaType, a window of a media type other than audio/wav or audio/mpeg, or
// without its MetaWindow, or an audio plan record that doesn't decode), a vector whose length is
// not Config.Dimensions, a refused request (ErrRequest), an invalid [Extract] among them, a text
// the chunker refuses (chunk.ErrRequest: a character that alone measures over Chunker.Bound), a
// chunker's count failing with a refused status, since no chunk holds that outcome, or a run
// asking for extraction of a pipeline without Config.Extraction. A [MissingError] is not
// permanent: a content store may hold the content on a redelivery. A transient error carries its
// *ai.APIError, whose RetryAfter is the redelivery's delay.
func Permanent(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := errors.AsType[*chunkError](err); ok && classify(err) == rowRefused {
		return true
	}
	if apiErr, ok := errors.AsType[*ai.APIError](err); ok {
		return statusRow(apiErr.Status) == rowPermanent
	}
	if _, ok := errors.AsType[*format.UnsupportedError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*dimensionsError](err); ok {
		return true
	}
	return errors.Is(err, dispatch.ErrSHA256) || errors.Is(err, dispatch.ErrRequest) || errors.Is(err, errInputDefect) ||
		errors.Is(err, ErrRequest) || errors.Is(err, chunk.ErrRequest) || errors.Is(err, errNoExtraction)
}
