package docinfer

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/metadata"
	"github.com/standards-lab/docproc/rundir/schema"
)

var metadataRecords = map[string]func([]byte) (string, error){
	dispatch.MediaOfficeMetadata:  renderAs(officeLines),
	dispatch.MediaPDFMetadata:     renderAs(pdfLines),
	dispatch.MediaMediaMetadata:   renderAs(mediaLines),
	dispatch.MediaPictureMetadata: renderAs(pictureLines),
}

type lines struct{ strings.Builder }

func (l *lines) add(label, value string) {
	if value != "" {
		l.WriteString(label + ": " + value + "\n")
	}
}

func (l *lines) date(label string, t time.Time) {
	if !t.IsZero() {
		l.add(label, utcText(t))
	}
}

func (l *lines) properties(props metadata.Properties) {
	for _, p := range props {
		l.add(p.Key, propertyText(p.Value))
	}
}

func renderAs[T any](render func(*lines, T)) func([]byte) (string, error) {
	return func(content []byte) (string, error) {
		var record T
		if err := json.Unmarshal(content, &record); err != nil {
			return "", err
		}
		var l lines
		render(&l, record)
		return l.String(), nil
	}
}

func utcText(t time.Time) string { return schema.TimeOf(t).Time().Format(time.RFC3339) }

func propertyText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return utcText(t)
		}
		return v
	case float64:
		return numberText(v)
	case bool:
		return strconv.FormatBool(v)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func officeLines(l *lines, r metadata.Office) {
	l.properties(r.Core)
	l.properties(r.Custom)
	for _, p := range r.Extended {
		if p.Key == "Company" || p.Key == "Manager" {
			l.add(p.Key, propertyText(p.Value))
		}
	}
	l.add("converted from", r.ConvertedFrom)
}

func pdfLines(l *lines, r metadata.PDF) {
	var info metadata.PDFInfo
	if r.Info != nil {
		info = *r.Info
	}
	l.add("title", info.Title)
	l.add("author", info.Author)
	l.add("subject", info.Subject)
	l.add("keywords", info.Keywords)
	l.date("creation date", info.CreationDate)
	l.date("modification date", info.ModDate)
	l.properties(info.Custom)
	if r.XMP == nil {
		return
	}
	if r.XMP.Title != info.Title {
		l.add("title", r.XMP.Title)
	}
	if !sameList(info.Author, r.XMP.Creators) {
		l.add("creators", strings.Join(r.XMP.Creators, ", "))
	}
	if r.XMP.Description != info.Subject {
		l.add("description", r.XMP.Description)
	}
	if !sameList(info.Keywords, r.XMP.Subjects) {
		l.add("subjects", strings.Join(r.XMP.Subjects, ", "))
	}
}

func mediaLines(l *lines, r metadata.Media) {
	if r.Duration != 0 {
		l.add("duration", numberText(r.Duration)+" seconds")
	}
	l.properties(r.Tags)
	for _, s := range r.Streams {
		l.properties(s.Tags)
	}
}

func pictureLines(l *lines, r metadata.Picture) {
	l.date("capture time", r.CaptureTime)
	l.add("make", r.Make)
	l.add("model", r.Model)
	if r.GPS != nil {
		l.add("GPS latitude", numberText(r.GPS.Latitude))
		l.add("GPS longitude", numberText(r.GPS.Longitude))
	}
}

func numberText(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func sameList(stored string, list []string) bool {
	if stored == strings.Join(list, ", ") {
		return true
	}
	var kept []string
	for _, it := range strings.FieldsFunc(stored, func(r rune) bool { return r == ',' || r == ';' }) {
		if it = strings.TrimSpace(it); it != "" {
			kept = append(kept, it)
		}
	}
	return slices.EqualFunc(kept, list, func(k, it string) bool { return k == strings.TrimSpace(it) })
}
