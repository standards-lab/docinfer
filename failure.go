package docinfer

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
)

// row is a go-ai failure's row of the failure table: what a model call's error comes to.
type row int

const (
	// rowRefused is a status that judges the content, 400, 413, 415, or 422: the item's outcome,
	// recorded and held.
	rowRefused row = iota
	// rowPermanent is a status that judges the caller or the route, or a request the format can't
	// send: it fails the same way on every delivery, so it stops the job, and Permanent reports it.
	rowPermanent
	// rowTransient is a status the gateway or an upstream may answer differently later, or a
	// transport failure: it stops the job, and a redelivery retries it after the reply's
	// RetryAfter.
	rowTransient
	// rowFailed is a 500, a failed or undecodable 2xx reply, or any other error a call returns: it
	// stops the job, but on the last delivery the item is recorded failed, never held, and the job
	// goes on.
	rowFailed
)

// classify returns err's row. err is a go-ai call's, made under a context that hasn't ended: a
// cancelled call is the job's cancellation, which no row holds.
func classify(err error) row {
	if apiErr, ok := errors.AsType[*ai.APIError](err); ok {
		return statusRow(apiErr.Status)
	}
	if _, ok := errors.AsType[*format.UnsupportedError](err); ok {
		return rowPermanent
	}
	// A reply that doesn't decode is the failed row's, though a truncated body's syntax error
	// matches io.ErrUnexpectedEOF too: the reply arrived whole, and decoded wrong.
	if _, ok := errors.AsType[*jsontext.SyntacticError](err); ok {
		return rowFailed
	}
	if _, ok := errors.AsType[*json.SemanticError](err); ok {
		return rowFailed
	}
	// A transport failure is a *url.Error from the client's Do, with no reply; a stream that breaks
	// matches io.ErrUnexpectedEOF, Options.StreamIdleTimeout's included.
	if _, ok := errors.AsType[*url.Error](err); ok {
		return rowTransient
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return rowTransient
	}
	return rowFailed
}

// statusRow returns an *ai.APIError's row by its status.
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
	// A status outside 4xx and 5xx that go-ai still reports as an error, such as a 3xx it didn't
	// follow, is no row's but "any other error a call returns".
	return rowFailed
}

// failureText is the text a refused or failed item records: the endpoint's reply, as its
// *ai.APIError reports it, or else the call's error.
func failureText(err error) string {
	if apiErr, ok := errors.AsType[*ai.APIError](err); ok {
		return apiErr.Error()
	}
	return err.Error()
}

// dimensionsError is a vector whose length isn't Config.Dimensions, made by the model or held in
// the cache: the embedding space is the profile's, so no redelivery changes it.
type dimensionsError struct {
	digest    string // the chunk's
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
// input (dispatch.ErrSHA256), an input's defect (an image output without a MediaType, a window of
// a media type other than audio/wav or audio/mpeg, or without its MetaWindow, or an audio plan
// record that doesn't decode), a vector whose length is not Config.Dimensions, a refused request
// (ErrRequest), an invalid [Extract] among them, or a run asking for extraction of a pipeline
// without Config.Extraction. A [MissingError] is not permanent: a content store may hold the
// content on a redelivery. A transient error carries its *ai.APIError, whose RetryAfter is the
// redelivery's delay.
func Permanent(err error) bool {
	if err == nil {
		return false
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
	return errors.Is(err, dispatch.ErrSHA256) || errors.Is(err, errInputDefect) || errors.Is(err, ErrRequest) ||
		errors.Is(err, errNoExtraction)
}
