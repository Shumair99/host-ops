package collector

import (
	"bytes"
	"encoding/json"
	"testing"
	"text/template"
)

func TestDockerProjectionWithoutHealthProducesDecodableJSON(t *testing.T) {
	fixture := map[string]any{"Id": "id", "Image": "sha256:image", "RestartCount": 2, "Name": "/web", "Path": "caddy", "Args": []string{"run"}, "Config": map[string]any{"Image": "caddy:2", "Labels": map[string]string{}, "Env": []string{"SECRET=private"}}, "State": map[string]any{"Status": "running", "Pid": 1, "StartedAt": "2026-01-01T00:00:00Z"}, "HostConfig": map[string]any{"RestartPolicy": map[string]string{"Name": "unless-stopped"}, "NetworkMode": "bridge"}, "NetworkSettings": map[string]any{"Networks": map[string]any{}, "Ports": map[string]any{}}, "Mounts": []any{}}
	parsed, err := template.New("inspect").Option("missingkey=error").Funcs(template.FuncMap{"json": func(value any) string { data, _ := json.Marshal(value); return string(data) }}).Parse(inspectProjection())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = parsed.Execute(&output, fixture); err != nil {
		t.Fatal(err)
	}
	var meta containerMeta
	if err = json.Unmarshal(output.Bytes(), &meta); err != nil {
		t.Fatal("projection is not valid inspect JSON:", err)
	}
	if meta.State.Status != "running" || meta.ID != "id" {
		t.Fatal("projected state was lost")
	}
	if bytes.Contains(output.Bytes(), []byte("private")) || meta.Image != "sha256:image" || meta.RestartCount != 2 {
		t.Fatal("projection leaked environment values or lost change evidence")
	}
}
