//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docinfer/cmd/docinfer/integration"
)

const probeTimeout = time.Minute

const media = "../../../integration/testdata"

const noteText = "The access code is seven four two nine. It opens the side door of the depot on weekdays."

func TestCLI_RunAndReport(t *testing.T) {
	eps := liveEndpoints(t)
	caps := capabilities(eps)
	for r, ep := range eps {
		if r != tokenize {
			ep.model, ep.served = model(t, r, ep)
		}
		eps[r] = ep
	}
	configDir := writeConfig(t, compose(eps, caps, dimensions(t, eps[embedding])))
	root := t.TempDir()
	docproc, out := filepath.Join(root, "docproc"), filepath.Join(root, "out")
	writeDocprocRun(t, docproc, caps)

	res := integration.Run(t, "run", docproc, "--out", out, "--report", "--config", configDir)

	if res.Code != process.ExitOK {
		t.Fatalf("run: code = %d, want %d; stderr = %q", res.Code, process.ExitOK, res.Stderr)
	}
	id, m := readManifest(t, out)
	if m.End == "" || m.Error != "" {
		t.Errorf("the manifest's end = %q and error = %q, want it ended with no error", m.End, m.Error)
	}
	var ran []string
	for _, c := range m.Capabilities {
		ran = append(ran, c.Capability)
	}
	for _, c := range caps {
		if !slices.Contains(ran, c.name) {
			t.Errorf("the manifest's capabilities are %q, want %s among them", ran, c.name)
		}
	}
	index := filepath.Join(out, "runs", id, "report", "index.html")
	if !strings.HasSuffix(res.Stdout, "\n"+index+"\n") {
		t.Errorf("run: stdout = %q, want it to end with the index's path, %s", res.Stdout, index)
	}
	if _, err := os.Stat(index); err != nil {
		t.Errorf("the index page: %v", err)
	}

	rep := integration.Run(t, "report", out)

	if rep.Code != process.ExitOK || rep.Stdout != index+"\n" {
		t.Errorf("report: code = %d and stdout = %q, want %d and the index's path, %s; stderr = %q",
			rep.Code, rep.Stdout, process.ExitOK, index, rep.Stderr)
	}
}

func liveEndpoints(t *testing.T) map[role]endpoint {
	t.Helper()
	eps := map[role]endpoint{}
	for _, r := range []role{embedding, vision, transcription, audio} {
		ep, ok, err := endpointOf(r, os.Getenv)
		switch {
		case err != nil:
			t.Fatal(err)
		case !ok && r == embedding:
			t.Skipf("%s is unset: set it to the embedding endpoint's API root, such as http://localhost:8000/v1", r.variable("BASE_URL"))
		case !ok:
			t.Logf("%s is unset: the run leaves its capability out", r.variable("BASE_URL"))
		default:
			eps[r] = ep
		}
	}
	switch ep, ok, err := endpointOf(tokenize, os.Getenv); {
	case err != nil:
		t.Fatal(err)
	case ok:
		eps[tokenize] = ep
	default:
		t.Logf("%s is unset: the run counts tokens through %s's tokenize route", tokenize.variable("BASE_URL"), embedding.variable("BASE_URL"))
	}
	return eps
}

func client(t *testing.T, r role, ep endpoint) *ai.Client {
	t.Helper()
	var opts ai.Options
	if token := ep.token; token != "" {
		opts.Token = func(context.Context) (string, error) { return token, nil }
	}
	for name, value := range ep.headers {
		if opts.Header == nil {
			opts.Header = http.Header{}
		}
		opts.Header.Set(name, value)
	}
	c, err := ai.New(ep.baseURL, &http.Client{}, completions.New(completions.Options{}), opts)
	if err != nil {
		t.Fatalf("New(%s=%q): %v", r.variable("BASE_URL"), ep.baseURL, err)
	}
	return c
}

func model(t *testing.T, r role, ep endpoint) (id, served string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()
	models, err := client(t, r, ep).Models(ctx)
	if _, ok := errors.AsType[*ai.APIError](err); ok && ep.model != "" {
		return ep.model, ep.model
	}
	if err != nil {
		t.Fatalf("Models of %s: %v", r.variable("BASE_URL"), err)
	}
	id = ep.model
	if id == "" {
		if len(models) != 1 || models[0].ID == "" {
			t.Fatalf("%s lists %d models, want exactly one, or its model named in %s", r.variable("BASE_URL"), len(models), r.variable("MODEL"))
		}
		id = models[0].ID
	}
	for _, m := range models {
		var entry struct {
			Root string `json:"root"`
		}
		if m.ID == id && json.Unmarshal(m.Native, &entry) == nil && entry.Root != "" {
			return id, ""
		}
	}
	return id, id
}

func dimensions(t *testing.T, ep endpoint) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()
	probe, err := client(t, embedding, ep).Embed(ctx, &ai.EmbeddingRequest{Model: ep.model, Input: []string{"A probe of the embedding space."}})
	if err != nil {
		t.Fatalf("Embed of the probe: %v", err)
	}
	if len(probe.Vectors) != 1 || len(probe.Vectors[0]) == 0 {
		t.Fatalf("Embed of the probe gave %d vectors, want one of at least one dimension", len(probe.Vectors))
	}
	return len(probe.Vectors[0])
}

func writeConfig(t *testing.T, files configFiles) string {
	t.Helper()
	dir := t.TempDir()
	for name, v := range map[string]any{"config.json": files.config, "secrets.json": files.secrets} {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeDocprocRun(t *testing.T, root string, caps []capability) {
	t.Helper()
	d, err := rundir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Begin(schema.Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	stage := dispatch.Identity{Name: "integration", Version: "1"}
	input := func(path string, data []byte, out dispatch.Output) {
		sum := sha256.Sum256(data)
		out.SHA256 = hex.EncodeToString(sum[:])
		if err := d.Content().Put(t.Context(), out.SHA256, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		part := dispatch.Part{ID: "/", Name: filepath.Base(path), Size: int64(len(data)), SHA256: out.SHA256}
		for _, e := range []schema.Event{
			{Kind: schema.OutputEvent, Input: path, Part: part, Stage: stage, Output: &out},
			{Kind: schema.PartEvent, Input: path, Part: part, Stage: stage, Outcome: dispatch.Prepared, Outputs: 1},
		} {
			if err := r.Write(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	input("/in/note.txt", []byte(noteText), dispatch.Output{Kind: dispatch.TextOutput, Name: "text", MediaType: "text/plain"})
	for _, c := range caps {
		switch c.name {
		case "vision":
			input("/in/shapes.png", readMedia(t, "shapes.png"),
				dispatch.Output{Kind: dispatch.ImageOutput, Name: "image", MediaType: "image/png"})
		case "transcription":
			input("/in/phrase.wav", readMedia(t, "phrase.wav"),
				dispatch.Output{Kind: dispatch.AudioOutput, Name: "window", MediaType: "audio/wav",
					Meta: map[string]string{dispatch.MetaWindow: "0"}})
		}
	}
	if err := r.End(nil); err != nil {
		t.Fatal(err)
	}
}

func readMedia(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(media, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type manifest struct {
	End          string `json:"end"`
	Error        string `json:"error"`
	Capabilities []struct {
		Capability string `json:"capability"`
	} `json:"capabilities"`
}

func readManifest(t *testing.T, out string) (string, manifest) {
	t.Helper()
	runs, err := os.ReadDir(filepath.Join(out, "runs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs/ holds %d entries, want the one run", len(runs))
	}
	id := runs[0].Name()
	b, err := os.ReadFile(filepath.Join(out, "runs", id, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	return id, m
}
