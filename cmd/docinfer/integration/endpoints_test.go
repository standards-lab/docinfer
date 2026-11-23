package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type role string

const (
	embedding     role = "AI_EMBEDDING"
	vision        role = "AI_VISION"
	transcription role = "AI_TRANSCRIPTION"
	audio         role = "AI_AUDIO"
)

func (r role) variable(suffix string) string { return string(r) + "_" + suffix }

type endpoint struct {
	baseURL string
	model   string
	token   string
	headers map[string]string
}

func endpointOf(r role, getenv func(string) string) (endpoint, bool, error) {
	base := getenv(r.variable("BASE_URL"))
	if base == "" {
		return endpoint{}, false, nil
	}
	headers, err := parseHeader(getenv(r.variable("HEADER")))
	if err != nil {
		return endpoint{}, false, fmt.Errorf("%s %w", r.variable("HEADER"), err)
	}
	return endpoint{baseURL: base, model: getenv(r.variable("MODEL")), token: getenv(r.variable("TOKEN")), headers: headers}, true, nil
}

func parseHeader(v string) (map[string]string, error) {
	var h map[string]string
	for i, line := range strings.Split(v, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf(`line %d has no colon: each line is "Name: value"`, i+1)
		}
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		if name == "" {
			return nil, fmt.Errorf(`line %d has no name before its colon: each line is "Name: value"`, i+1)
		}
		if _, dup := h[name]; dup {
			return nil, fmt.Errorf("line %d names %s again: docinfer's headers carry one value per name", i+1, name)
		}
		if h == nil {
			h = map[string]string{}
		}
		h[name] = strings.TrimSpace(value)
	}
	return h, nil
}

type capability struct {
	name string
	role role
	mode string
}

type configFiles struct {
	config, secrets map[string]any
}

func compose(eps map[role]endpoint, caps []capability, dims int) configFiles {
	endpoints, secrets := map[string]any{}, map[string]any{}
	config := map[string]any{"endpoints": endpoints}
	for _, c := range caps {
		ep := eps[c.role]
		name := strings.ToLower(strings.TrimPrefix(string(c.role), "AI_"))
		endpoints[name] = map[string]any{"base_url": ep.baseURL, "format": "completions"}
		if ep.token != "" || len(ep.headers) > 0 {
			secret := map[string]any{}
			if ep.token != "" {
				secret["token"] = ep.token
			}
			if len(ep.headers) > 0 {
				secret["headers"] = ep.headers
			}
			secrets[name] = secret
		}
		section := map[string]any{"endpoint": name, "model": ep.model}
		switch c.name {
		case "vision":
			section["max_tokens"] = visionTokens
		case "transcription":
			section["mode"] = c.mode
		case "embeddings":
			section["dimensions"] = dims
		}
		config[c.name] = section
	}
	return configFiles{config: config, secrets: map[string]any{"endpoints": secrets}}
}

const visionTokens = 256

func capabilities(eps map[role]endpoint) []capability {
	caps := []capability{{name: "embeddings", role: embedding}}
	if _, ok := eps[vision]; ok {
		caps = append(caps, capability{name: "vision", role: vision})
	}
	if _, ok := eps[transcription]; ok {
		caps = append(caps, capability{name: "transcription", role: transcription, mode: "route"})
	} else if _, ok := eps[audio]; ok {
		caps = append(caps, capability{name: "transcription", role: audio, mode: "chat"})
	}
	return caps
}

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestEndpointOf(t *testing.T) {
	vars := map[string]string{
		"AI_VISION_BASE_URL": "http://gb10:8080/v1",
		"AI_VISION_MODEL":    "gemma",
		"AI_VISION_TOKEN":    "tok",
		"AI_VISION_HEADER":   "\n  api-key :  key \r\n\nx-route:a:b\n",
	}

	got, ok, err := endpointOf(vision, env(vars))
	if err != nil || !ok {
		t.Fatalf("endpointOf = %v, %v, want the endpoint", ok, err)
	}
	want := endpoint{baseURL: "http://gb10:8080/v1", model: "gemma", token: "tok",
		headers: map[string]string{"Api-Key": "key", "X-Route": "a:b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("endpointOf = %+v, want %+v", got, want)
	}
	if _, ok, err := endpointOf(audio, env(vars)); ok || err != nil {
		t.Errorf("endpointOf(audio) = %v, %v, want it unset", ok, err)
	}
}

const secret = "s3cret-value"

func TestEndpointOf_HeaderRefusedWithoutItsValue(t *testing.T) {
	for _, tc := range []struct {
		name, header, want string
	}{
		{"no colon", "api-key: ok\n" + secret, "AI_AGENT_HEADER line 2 has no colon"},
		{"no name", ": " + secret, "AI_AGENT_HEADER line 1 has no name"},
		{"a name twice", "x-tag: one\nX-Tag: " + secret, "AI_AGENT_HEADER line 2 names X-Tag again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vars := map[string]string{"AI_AGENT_BASE_URL": "http://localhost:8000/v1", "AI_AGENT_HEADER": tc.header}

			_, _, err := endpointOf("AI_AGENT", env(vars))

			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to start with %q", err, tc.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("err = %q, want it never to print a header's value", err)
			}
		})
	}
}

func TestCompose_CredentialsOnlyInSecrets(t *testing.T) {
	eps := map[role]endpoint{
		embedding: {baseURL: "http://gb10:8080/v1", model: "embed", token: secret},
		audio:     {baseURL: "http://gb10:8081/v1", model: "gemma", headers: map[string]string{"Api-Key": secret}},
	}

	files := compose(eps, capabilities(eps), 768)

	config, err := json.Marshal(files.config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), secret) {
		t.Errorf("config.json = %s, want no token or header in it", config)
	}
	want := `{"embeddings":{"dimensions":768,"endpoint":"embedding","model":"embed"},` +
		`"endpoints":{"audio":{"base_url":"http://gb10:8081/v1","format":"completions"},` +
		`"embedding":{"base_url":"http://gb10:8080/v1","format":"completions"}},` +
		`"transcription":{"endpoint":"audio","mode":"chat","model":"gemma"}}`
	if string(config) != want {
		t.Errorf("config.json = %s, want %s", config, want)
	}
	secrets, err := json.Marshal(files.secrets)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"endpoints":{"audio":{"headers":{"Api-Key":"` + secret + `"}},"embedding":{"token":"` + secret + `"}}}`; string(secrets) != want {
		t.Errorf("secrets.json = %s, want %s", secrets, want)
	}
}

func TestCapabilities_RouteBeforeChat(t *testing.T) {
	eps := map[role]endpoint{embedding: {}, vision: {}, transcription: {}, audio: {}}

	got := capabilities(eps)

	want := []capability{{"embeddings", embedding, ""}, {"vision", vision, ""}, {"transcription", transcription, "route"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("capabilities = %+v, want %+v", got, want)
	}
}
