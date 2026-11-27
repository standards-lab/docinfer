package docinfer_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/aitest"
)

var (
	docxPart  = dispatch.Part{ID: "/", Name: "plan.docx", Size: 4096, SHA256: strings.Repeat("c", 64)}
	pdfPart   = dispatch.Part{ID: "/", Name: "plan.pdf", Size: 8192, SHA256: strings.Repeat("d", 64)}
	pdfStage  = dispatch.Identity{Name: "document", Version: "1", Config: "c1"}
	jpegPart  = dispatch.Part{ID: "/", Name: "harbor.jpg", Size: 2048, SHA256: strings.Repeat("e", 64)}
	jpegStage = dispatch.Identity{Name: "picture", Version: "1", Config: "p0"}
	pstPart   = dispatch.Part{ID: "/", Name: "mail.pst", Size: 65536, SHA256: strings.Repeat("f", 64)}
	pstStage  = dispatch.Identity{Name: "email", Version: "1", Config: "m0"}
	onePart   = dispatch.Part{ID: "/", Name: "notes.one", Size: 32768, SHA256: strings.Repeat("0", 64)}
	oneStage  = dispatch.Identity{Name: "onenote", Version: "1", Config: "o0"}
	officeRec = `{"core":{"title":"Quarterly Report","creator":"Ann Lee","created":"2026-03-15T09:30:00+05:30",` +
		`"lastModifiedBy":"Bo Chan"},` +
		`"extended":{"Application":"Microsoft Office Word","Pages":3,"Company":"Acme","Manager":"Cy Diaz"},` +
		`"custom":{"Project":"bunrei","Reviewed":true,"Ratio":0.25},"converted_from":"doc"}`
	officeText = "title: Quarterly Report\n" +
		"creator: Ann Lee\n" +
		"created: 2026-03-15T04:00:00Z\n" +
		"lastModifiedBy: Bo Chan\n" +
		"Project: bunrei\n" +
		"Reviewed: true\n" +
		"Ratio: 0.25\n" +
		"Company: Acme\n" +
		"Manager: Cy Diaz\n" +
		"converted from: doc\n"
	pdfRec = `{"version":"1.7","info":{"title":"Annual Plan","author":"Ann Lee; Bo Chan","subject":"Budget",` +
		`"keywords":"plan, budget","creator":"Writer","producer":"LibreOffice 7.6",` +
		`"creation_date":"2026-03-15T04:00:00Z","mod_date":"2026-03-16T05:30:00Z","custom":{"Department":"Finance"}},` +
		`"xmp":{"title":"Annual Plan","creators":["Ann Lee","Bo Chan"],"description":"The year's plan",` +
		`"subjects":["plan","budget","2026"],"languages":["en"],"create_date":"2026-03-15T04:00:00Z",` +
		`"creator_tool":"Writer","producer":"LibreOffice 7.6","document_id":"uuid:1","packet":"<x:xmpmeta>packet</x:xmpmeta>"}}`
	pdfText = "title: Annual Plan\n" +
		"author: Ann Lee; Bo Chan\n" +
		"subject: Budget\n" +
		"keywords: plan, budget\n" +
		"creation date: 2026-03-15T04:00:00Z\n" +
		"modification date: 2026-03-16T05:30:00Z\n" +
		"Department: Finance\n" +
		"description: The year's plan\n" +
		"subjects: plan, budget, 2026\n"
	mediaRec = `{"format":"mp3","duration":245.32,"bit_rate":320000,"tags":{"major_brand":"isom","title":"Morning Song","Minor_Version":"512",` +
		`"encoder":"Lavf60.16.100","COMPATIBLE_BRANDS":"isomiso2avc1mp41","artist":"Ann Lee","Encoded_By":"LAME"},` +
		`"streams":[{"index":0,"type":"audio","codec":"mp3","sample_rate":44100,"channels":2,"tags":{"language":"eng",` +
		`"ENCODER":"Lavc60","Handler_Name":"SoundHandler","vendor_id":"[0][0][0][0]","BPS":"320000","DURATION":"00:04:05.320000000",` +
		`"number_of_bytes":"9812800","Number_Of_Frames":"9393","_STATISTICS_WRITING_APP":"mkvmerge v80.0",` +
		`"_statistics_writing_date_utc":"2026-03-15 09:30:00","_Statistics_Tags":"BPS DURATION NUMBER_OF_FRAMES NUMBER_OF_BYTES"}},` +
		`{"index":1,"type":"video","codec":"mjpeg","width":500,"height":500,"frame_rate":90000,"attached_picture":true,` +
		`"tags":{"comment":"Cover (front)"}}]}`
	mediaText = "duration: 245.32 seconds\n" +
		"title: Morning Song\n" +
		"artist: Ann Lee\n" +
		"language: eng\n" +
		"DURATION: 00:04:05.320000000\n" +
		"comment: Cover (front)\n"
	pictureRec = `{"capture_time":"2026-03-15T09:30:00+05:30","make":"Canon","model":"Canon EOS R5","orientation":6,` +
		`"width":8192,"height":5464,"gps":{"latitude":40.6892,"longitude":-74.0445},` +
		`"exif":{"DateTimeOriginal":"2026:03:15 09:30:00","Make":"Canon","Model":"Canon EOS R5","Software":"Firmware 1.8"}}`
	pictureText = "capture time: 2026-03-15T04:00:00Z\n" +
		"make: Canon\n" +
		"model: Canon EOS R5\n" +
		"GPS latitude: 40.6892\n" +
		"GPS longitude: -74.0445\n"
	messageRec = `{"id":2097188,"folder":32802,"class":"IPM.Note","subject":"Budget review",` +
		`"sender":{"name":"Ann Lee","address":"/O=ACME/OU=EXCHANGE/CN=ANN","address_type":"EX","smtp_address":"ann@example.com"},` +
		`"to":[{"name":"Bo Chan","address":"bo@example.com","address_type":"SMTP"},{"address":"cy@example.com","address_type":"SMTP"}],` +
		`"cc":[{"name":"Dee Ray","address_type":"EX"}],"bcc":[{"name":"Eve Fox","address":"eve@example.com","address_type":"SMTP"}],` +
		`"submit_time":"2026-03-15T09:30:00+05:30","delivery_time":"2026-03-15T04:00:05Z","creation_time":"2026-03-15T03:59:00Z",` +
		`"modification_time":"2026-03-15T04:01:00Z","importance":"high","sensitivity":"private",` +
		`"transport_headers":"From: Ann Lee <ann@example.com>\r\nReceived: from mx.example.com\r\n",` +
		`"message_id":"<m1@example.com>","in_reply_to":"<m0@example.com>","references":"<m0@example.com>",` +
		`"conversation_topic":"Budget talks","conversation_index":"01d2a3",` +
		`"markdown":"The body of the message.",` +
		`"attachments":[{"file":"` + strings.Repeat("a", 64) + `.pdf","filename":"plan.pdf","mime_type":"application/pdf","method":"by_value","size":10}],` +
		`"media":[{"file":"` + strings.Repeat("b", 64) + `.png","mime_type":"image/png"}]}`
	messageText = "from: Ann Lee <ann@example.com>\n" +
		"to: Bo Chan <bo@example.com>, cy@example.com\n" +
		"cc: Dee Ray\n" +
		"bcc: Eve Fox <eve@example.com>\n" +
		"sent: 2026-03-15T04:00:00Z\n" +
		"subject: Budget review\n"
	deliveredRec  = `{"subject":"Re: Budget review","sender":{"name":"Bo Chan","address":"bo@example.com"},"delivery_time":"2026-03-16T10:00:00+05:30"}`
	deliveredText = "from: Bo Chan <bo@example.com>\n" +
		"sent: 2026-03-16T04:30:00Z\n" +
		"subject: Re: Budget review\n"
	pageRec = `{"id":"6f1c2d3e-0000-4000-8000-000000000001","section":"6f1c2d3e-0000-4000-8000-000000000002","level":1,` +
		`"title":"Harbor notes","creation_time":"2026-03-15T09:30:00+05:30","modification_time":"2026-03-16T05:30:00Z",` +
		`"author":"Ann Lee","markdown":"The page's text.",` +
		`"media":[{"kind":"image","file":"` + strings.Repeat("c", 64) + `.png","filename":"harbor.png","size":2048,"extension":"png"}],` +
		`"ink_alternatives":[{"word":0,"alternatives":["hat"]}]}`
	pageText = "title: Harbor notes\n" +
		"author: Ann Lee\n" +
		"created: 2026-03-15T04:00:00Z\n" +
		"modified: 2026-03-16T05:30:00Z\n"
)

func withMeta(in docinfer.Input, meta map[string]string) docinfer.Input {
	in.Event.Output.Meta = meta
	return in
}

func recordInput(t *testing.T, content *dispatch.MemoryContent, part dispatch.Part, stage dispatch.Identity, mediaType, record string) docinfer.Input {
	t.Helper()
	in := outputInput(t, content, part, dispatch.RecordOutput, record)
	in.Event.Stage = stage
	in.Event.Output.Name = "metadata"
	in.Event.Output.MediaType = mediaType
	return in
}

func TestRunRendersMetadataRecords(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	cfg := extractConfig(t, e, nil)
	p := newPipeline(t, cfg)
	content := dispatch.NewMemoryContent()
	inputs := []docinfer.Input{
		recordInput(t, content, docxPart, textStage, dispatch.MediaOfficeMetadata, officeRec),
		recordInput(t, content, pdfPart, pdfStage, dispatch.MediaPDFMetadata, pdfRec),
		recordInput(t, content, audioPart, audioStage, dispatch.MediaMediaMetadata, mediaRec),
		recordInput(t, content, jpegPart, jpegStage, dispatch.MediaPictureMetadata, pictureRec),
		withMeta(recordInput(t, content, pstPart, pstStage, dispatch.MediaMail, messageRec),
			map[string]string{dispatch.MetaSubject: "Budget review", dispatch.MetaID: "2097188", dispatch.MetaFolder: "Inbox",
				dispatch.MetaMessageID: "<m1@example.com>", dispatch.MetaInReplyTo: "<m0@example.com>", dispatch.MetaReferences: "<m0@example.com>"}),
		withMeta(recordInput(t, content, pstPart, pstStage, dispatch.MediaMail, deliveredRec),
			map[string]string{dispatch.MetaSubject: "Re: Budget review", dispatch.MetaEmbeddedIn: "message 2097188"}),
		withMeta(recordInput(t, content, onePart, oneStage, dispatch.MediaOneNote, pageRec),
			map[string]string{dispatch.MetaTitle: "Harbor notes", dispatch.MetaID: "6f1c2d3e-0000-4000-8000-000000000001",
				dispatch.MetaSection: "6f1c2d3e-0000-4000-8000-000000000002"}),
		recordInput(t, content, childPart, textStage, dispatch.MediaForm, `{"fields":[]}`),
		withMeta(recordInput(t, content, pstPart, pstStage, dispatch.MediaMail, `{"schema":"2","format":"pst","encoding":"unicode","display_name":"Ann's mail"}`),
			map[string]string{dispatch.MetaFormat: "pst"}),
		withMeta(recordInput(t, content, pstPart, pstStage, dispatch.MediaMail, `{"id":32802,"path":["Inbox"],"container_class":"IPF.Note"}`),
			map[string]string{dispatch.MetaID: "32802", dispatch.MetaPath: "Inbox"}),
		recordInput(t, content, pstPart, pstStage, dispatch.MediaMail, `{"subject":"Planning","sender":{"name":"Ann Lee"}}`),
		recordInput(t, content, onePart, oneStage, dispatch.MediaOneNote, `{"title":"Trips","author":"Ann Lee"}`),
		withMeta(recordInput(t, content, pstPart, pstStage, dispatch.MediaMailConversation,
			`{"id":2097200,"class":"IPM.Note.Microsoft.Conversation","topic":"Standup","participants":[{"name":"Ann Lee"}],`+
				`"start":"2026-03-15T04:00:00Z","markdown":"Ann: hello"}`),
			map[string]string{dispatch.MetaTopic: "Standup", dispatch.MetaMissed: "false", dispatch.MetaID: "2097200"}),
		withMeta(recordInput(t, content, onePart, oneStage, dispatch.MediaOneNote, `{"schema":"2","input":"notebook","name":"Field notes","id":"6f1c2d3e-0000-4000-8000-000000000000"}`),
			map[string]string{dispatch.MetaID: "6f1c2d3e-0000-4000-8000-000000000000", dispatch.MetaName: "Field notes"}),
		withMeta(recordInput(t, content, onePart, oneStage, dispatch.MediaOneNote, `{"id":"6f1c2d3e-0000-4000-8000-000000000003","path":["Trips"]}`),
			map[string]string{dispatch.MetaID: "6f1c2d3e-0000-4000-8000-000000000003", dispatch.MetaPath: "Trips"}),
		withMeta(recordInput(t, content, onePart, oneStage, dispatch.MediaOneNote,
			`{"id":"6f1c2d3e-0000-4000-8000-000000000002","parent":"6f1c2d3e-0000-4000-8000-000000000003","path":["Trips","Harbor"],"color":"#8aa8e4"}`),
			map[string]string{dispatch.MetaID: "6f1c2d3e-0000-4000-8000-000000000002", dispatch.MetaPath: "Trips/Harbor"}),
	}

	o := collect(t, p.Run(t.Context(), content, seq(inputs...), extracting(false)))
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	got := o.chunks()
	var all int
	for _, w := range []struct {
		text  string
		in    docinfer.Input
		stage dispatch.Identity
	}{
		{officeText, inputs[0], textStage},
		{pdfText, inputs[1], pdfStage},
		{mediaText, inputs[2], audioStage},
		{pictureText, inputs[3], jpegStage},
		{messageText, inputs[4], pstStage},
		{deliveredText, inputs[5], pstStage},
		{pageText, inputs[6], oneStage},
	} {
		pieces, err := cfg.Chunker.Split(t.Context(), w.text)
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, g := range got {
			if g.source.Output == nil || g.source.Output.SHA256 != w.in.Event.Output.SHA256 {
				continue
			}
			texts = append(texts, g.chunk.Text)
			if g.source.Kind != "metadata" || g.source.SHA256 != sha(w.text) || !reflect.DeepEqual(*g.source.Output, *w.in.Event.Output) ||
				g.source.Stage == nil || *g.source.Stage != w.stage || g.part.ID != w.in.Event.Part.ID || g.part.Name != w.in.Event.Part.Name {
				t.Errorf("chunk %q: source %+v of part %+v, want kind metadata, the rendered text's SHA-256, the record output, and its part",
					g.chunk.Text, g.source, g.part)
			}
		}
		var want []string
		for _, c := range pieces {
			want = append(want, c.Text)
		}
		slices.Sort(texts)
		slices.Sort(want)
		if !slices.Equal(texts, want) {
			t.Errorf("record %s: chunks %q, want those of %q", w.in.Event.Output.MediaType, texts, w.text)
		}
		all += len(want)
	}
	if len(got) != all {
		t.Errorf("Run yielded %d chunks, want the records' %d", len(got), all)
	}
	for _, b := range o.batches {
		checkBatch(t, b)
	}
	if n := chatRequests(e); n != 0 {
		t.Errorf("%d extraction requests, want none for metadata records", n)
	}
	if o.counts == nil || o.counts.Metadata != 7 || o.counts.Texts != 0 || o.counts.Embedded != all ||
		o.counts.Extraction == nil || *o.counts.Extraction != (docinfer.ExtractionCounts{}) {
		t.Errorf("counts %+v, want seven metadata texts, their chunks embedded, and nothing extracted", o.counts)
	}
	if len(o.settled) != len(inputs) {
		t.Errorf("settled %v, want every input", o.settled)
	}
}

func TestRunMetadataRecordWithNoLines(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	content := dispatch.NewMemoryContent()
	inputs := []docinfer.Input{
		recordInput(t, content, docxPart, textStage, dispatch.MediaOfficeMetadata,
			`{"extended":{"Application":"Microsoft Office Word","Pages":3},"custom":{}}`),
		recordInput(t, content, pdfPart, pdfStage, dispatch.MediaPDFMetadata,
			`{"version":"1.4","info":{"creator":"Writer","producer":"LibreOffice"},"xmp":{"creator_tool":"Writer","packet":"<x/>"}}`),
		recordInput(t, content, audioPart, audioStage, dispatch.MediaMediaMetadata,
			`{"format":"flac","bit_rate":900000,"streams":[{"index":0,"type":"audio","codec":"flac","sample_rate":48000,"channels":2}]}`),
		recordInput(t, content, jpegPart, jpegStage, dispatch.MediaPictureMetadata,
			`{"orientation":1,"width":640,"height":480,"exif":{"Software":"Firmware 1.8"}}`),
		withMeta(recordInput(t, content, pstPart, pstStage, dispatch.MediaMail,
			`{"id":7,"class":"IPM.Note","markdown":"A body.","message_id":"<m7@example.com>","transport_headers":"X-A: b\r\n"}`),
			map[string]string{dispatch.MetaSubject: "", dispatch.MetaID: "7"}),
		withMeta(recordInput(t, content, onePart, oneStage, dispatch.MediaOneNote,
			`{"id":"6f1c2d3e-0000-4000-8000-000000000009","level":1,"markdown":"Ink."}`),
			map[string]string{dispatch.MetaTitle: "", dispatch.MetaID: "6f1c2d3e-0000-4000-8000-000000000009"}),
	}
	o := collect(t, newPipeline(t, runConfig(t, e, nil)).Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	if len(o.batches) != 0 || len(e.Requests()) != 0 {
		t.Errorf("%d batches and %d requests, want none", len(o.batches), len(e.Requests()))
	}
	if !slices.Equal(slices.Sorted(slices.Values(o.settled)), []int{0, 1, 2, 3, 4, 5}) {
		t.Errorf("settled %v, want every record", o.settled)
	}
	checkCounts(t, o, docinfer.Counts{})
}

func TestRunMetadataRecordUndecodable(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	for _, r := range []struct {
		mediaType string
		meta      map[string]string
	}{
		{dispatch.MediaOfficeMetadata, nil},
		{dispatch.MediaPDFMetadata, nil},
		{dispatch.MediaMediaMetadata, nil},
		{dispatch.MediaPictureMetadata, nil},
		{dispatch.MediaMail, map[string]string{dispatch.MetaSubject: "Budget review"}},
		{dispatch.MediaOneNote, map[string]string{dispatch.MetaTitle: "Harbor notes"}},
	} {
		t.Run(r.mediaType, func(t *testing.T) {
			content := dispatch.NewMemoryContent()
			in := withMeta(recordInput(t, content, childPart, textStage, r.mediaType, `{"core":`), r.meta)
			o := collect(t, newPipeline(t, runConfig(t, e, nil)).Run(t.Context(), content, seq(in), docinfer.Options{Final: true}))
			if o.err == nil || !docinfer.Permanent(o.err) {
				t.Fatalf("Run: %v, want a permanent error", o.err)
			}
			if msg := o.err.Error(); !strings.Contains(msg, in.Event.Output.SHA256) || !strings.Contains(msg, childPart.ID) ||
				!strings.Contains(msg, "unexpected end of JSON input") {
				t.Errorf("Run: %v, want the record's SHA-256, its part, and the cause", o.err)
			}
		})
	}
	if n := len(e.Requests()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}
}

func TestRunMetadataRecordMissing(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	in := recordInput(t, nil, docxPart, textStage, dispatch.MediaOfficeMetadata, officeRec)
	o := collect(t, newPipeline(t, runConfig(t, e, nil)).Run(t.Context(), dispatch.NewMemoryContent(), seq(in), docinfer.Options{}))
	if m, ok := errors.AsType[*docinfer.MissingError](o.err); !ok || docinfer.Permanent(o.err) || m.Event.Output.SHA256 != in.Event.Output.SHA256 {
		t.Errorf("Run: %v, want a *MissingError of the record, not permanent", o.err)
	}
}
