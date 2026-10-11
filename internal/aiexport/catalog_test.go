package aiexport

import (
	"encoding/csv"
	"encoding/json"
	"html"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func decodeEvidence(t *testing.T, raw string) any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		t.Fatalf("invalid evidence JSON: %v: %s", err, raw)
	}
	return value
}

// Reconstruct from the rendered Markdown, not just the in-memory catalog, to
// catch dropped fields, escaped text, CSV gaps and reference/provenance errors.
func renderedRecords(t *testing.T, markdown string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	pattern := regexp.MustCompile(`^\| ([A-Z][0-9]+) \| .* \| (<code>.*</code>) \|$`)
	for _, line := range strings.Split(markdown, "\n") {
		matches := pattern.FindStringSubmatch(line)
		if len(matches) != 3 || strings.HasPrefix(matches[1], "V") {
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(matches[2], "<code>"), "</code>")
		out[matches[1]] = decodeEvidence(t, html.UnescapeString(raw)).(map[string]any)
	}
	return out
}

func renderedSeries(t *testing.T, markdown string) map[string][]map[string]any {
	t.Helper()
	out := map[string][]map[string]any{}
	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "### T") {
			continue
		}
		id := strings.TrimPrefix(line, "### ")
		start := i + 1
		for start < len(lines) && !strings.HasSuffix(lines[start], "csv") {
			start++
		}
		if start >= len(lines) {
			t.Fatal("time-series CSV missing", id)
		}
		fence := strings.TrimSuffix(lines[start], "csv")
		end := start + 1
		for end < len(lines) && lines[end] != fence {
			end++
		}
		if end == len(lines) {
			t.Fatal("time-series fence damaged", id)
		}
		rows, err := csv.NewReader(strings.NewReader(strings.Join(lines[start+1:end], "\n"))).ReadAll()
		if err != nil || len(rows) < 2 {
			t.Fatal("CSV damaged", err)
		}
		header := rows[0]
		for _, cells := range rows[1:] {
			row := decodeEvidence(t, cells[len(cells)-1]).(map[string]any)
			row["observed_at"] = cells[0]
			row["values"] = nil
			if cells[1] == "object" {
				values := map[string]any{}
				for column := 2; column < len(header)-1; column++ {
					if cells[column] != "" {
						values[header[column]] = decodeEvidence(t, cells[column])
					}
				}
				row["values"] = values
			}
			out[id] = append(out[id], row)
		}
	}
	return out
}

func expandEvidence(value any, records map[string]map[string]any, series map[string][]map[string]any) any {
	switch v := value.(type) {
	case map[string]any:
		if id, ok := v["$ref"].(string); ok && len(v) == 1 {
			out := map[string]any{}
			for key, child := range records[id] {
				out[key] = expandEvidence(child, records, series)
			}
			return out
		}
		if id, ok := v["$series"].(string); ok && len(v) == 2 {
			out := []any{}
			for _, sample := range series[id] {
				row := map[string]any{}
				for key, child := range sample {
					row[key] = child
				}
				for key, child := range v["scope"].(map[string]any) {
					row[key] = child
				}
				out = append(out, row)
			}
			return out
		}
		out := map[string]any{}
		for key, child := range v {
			out[key] = expandEvidence(child, records, series)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = expandEvidence(child, records, series)
		}
		return out
	default:
		return v
	}
}

func TestConsolidatedEvidenceIsLosslessAndSharesFactsAndAliasHistories(t *testing.T) {
	now := time.Now().UTC()
	rule := map[string]any{"id": "backup", "title": "Recent backup", "state": "unknown", "evidence": "Not recorded", "points": 0, "max_points": 10}
	check := map[string]any{"site_id": "a", "state": "warn", "layer": "dns", "summary": "Observed  two spaces | and `ticks`\nnext line"}
	history := func(site string) []any {
		return []any{
			map[string]any{"host_id": "h", "site_id": site, "observed_at": "2026-10-11T12:00:00Z", "values": map[string]any{"cpu_percent": 0, "bytes": uint64(9007199254740993)}, "unavailable": []string{}},
			map[string]any{"host_id": "h", "site_id": site, "observed_at": "2026-10-11T12:00:10Z", "values": map[string]any{"cpu_percent": 1.5}, "unavailable": []string{"disk"}, "extra": "```\n# untrusted\n```"},
			map[string]any{"host_id": "h", "site_id": site, "observed_at": "2026-10-11T12:00:20Z", "values": nil, "unavailable": nil},
			map[string]any{"host_id": "h", "site_id": site, "observed_at": "2026-10-11T12:00:30Z", "values": map[string]any{}, "extra": map[string]any{}},
		}
	}
	sections := []Section{
		{Title: "Overview", Data: map[string]any{"domain_labels": map[string]string{"a": "example.com", "b": "www.example.com"}}},
		{Title: "Sites", Data: map[string]any{"health": []any{check}, "histories": map[string]any{"a": history("a"), "b": history("b")}, "reserved": map[string]any{"$ref": "C001"}, "empty": []any{}, "nil": nil}},
		{Title: "Issues", Data: []any{check}},
		{Title: "Readiness", Data: []any{map[string]any{"site_id": "a", "rules": []any{rule}}, map[string]any{"site_id": "b", "rules": []any{rule}}}},
		{Title: "Unavailable", State: "unavailable", Data: nil},
		{Title: "Non-metric", Data: []any{map[string]any{"observed_at": "time", "other": 1}}},
	}
	catalog, err := buildCatalog(sections)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Render(Document{StartedAt: now, FinishedAt: now, Sections: sections})
	if err != nil {
		t.Fatal(err)
	}
	markdown := string(data)
	records, series := renderedRecords(t, markdown), renderedSeries(t, markdown)
	if len(series) != 1 || len(catalog.Series) != 1 {
		t.Fatal("identical alias measurements were duplicated", len(series))
	}
	for _, source := range catalog.Sources {
		expected, err := jsonValue(source.Section.Data)
		if err != nil {
			t.Fatal(err)
		}
		actual := expandEvidence(source.Root, records, series)
		if !reflect.DeepEqual(expected, actual) {
			t.Fatalf("%s lost or changed evidence\nexpected: %s\nactual: %s", source.Section.Title, compact(expected), compact(actual))
		}
	}
	sharedCheck, sharedRule := false, false
	for _, record := range catalog.Records {
		if record.Fields["layer"] == "dns" {
			sharedCheck = record.Sources["V02"] && record.Sources["V03"]
		}
		if record.Fields["id"] == "backup" {
			sharedRule = record.Subjects["example.com"] && record.Subjects["www.example.com"]
		}
	}
	if !sharedCheck || !sharedRule {
		t.Fatal("shared evidence lost source/subject provenance", sharedCheck, sharedRule)
	}
}
