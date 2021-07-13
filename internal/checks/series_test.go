package checks_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudflare/pint/internal/checks"
	"github.com/cloudflare/pint/internal/parser"

	"github.com/rs/zerolog"
)

func TestSeriesCheck(t *testing.T) {
	zerolog.SetGlobalLevel(zerolog.FatalLevel)

	p := parser.NewParser()
	rules, err := p.Parse([]byte(`groups:
  - name: testinggroup
    rules:
      - record: notfound
        labels:
          foo: bar
        expr: vector(1)`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatal("failed to parse rules")
	}
	rrSet := []*parser.RecordingRule{rules[0].RecordingRule}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			t.Fatal(err)
		}
		query := r.Form.Get("query")

		switch query {
		case "count(notfound)":
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[]
				}
			}`))
		case "count(found_1)":
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[{"metric":{},"value":[1614859502.068,"1"]}]
				}
			}`))
		case "count(found_7)":
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[{"metric":{},"value":[1614859502.068,"7"]}]
				}
			}`))
		case `count(node_filesystem_readonly{mountpoint!=""})`:
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[{"metric":{},"value":[1614859502.068,"1"]}]
				}
			}`))
		case `count(disk_info{interface_speed!="6.0 Gb/s",type="sat"})`:
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[{"metric":{},"value":[1614859502.068,"1"]}]
				}
			}`))
		case `count(found{job="notfound"})`, `count(notfound{job="notfound"})`:
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[]
				}
			}`))
		case `count(found)`:
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[{"metric":{},"value":[1614859502.068,"1"]}]
				}
			}`))
		default:
			w.WriteHeader(400)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"error",
				"errorType":"bad_data",
				"error":"unhandled query"
			}`))
		}
	}))
	defer srv.Close()

	testCases := []checkTest{
		{
			description: "ignores rules with syntax errors",
			content:     "- record: foo\n  expr: sum(foo) without(\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Warning, false, nil),
		},
		{
			description: "bad response",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Warning, false, nil),
			problems: []checks.Problem{
				{
					Fragment: "foo",
					Lines:    []int{2},
					Reporter: "query/series",
					Text:     "query using prom failed with: bad_data: unhandled query",
					Severity: checks.Bug,
				},
			},
		},
		{
			description: "simple query",
			content:     "- record: foo\n  expr: sum(notfound)\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Warning, false, nil),
			problems: []checks.Problem{
				{
					Fragment: "notfound",
					Lines:    []int{2},
					Reporter: "query/series",
					Text:     "query using prom completed without any results for notfound",
					Severity: checks.Warning,
				},
			},
		},
		{
			description: "complex query",
			content:     "- record: foo\n  expr: sum(found_7 * on (job) sum(sum(notfound))) / found_7\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Warning, false, nil),
			problems: []checks.Problem{
				{
					Fragment: "notfound",
					Lines:    []int{2},
					Reporter: "query/series",
					Text:     "query using prom completed without any results for notfound",
					Severity: checks.Warning,
				},
			},
		},
		{
			description: "complex query / bug",
			content:     "- record: foo\n  expr: sum(found_7 * on (job) sum(sum(notfound))) / found_7\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Bug, false, nil),
			problems: []checks.Problem{
				{
					Fragment: "notfound",
					Lines:    []int{2},
					Reporter: "query/series",
					Text:     "query using prom completed without any results for notfound",
					Severity: checks.Bug,
				},
			},
		},
		{
			description: "complex query / bug but recording rule present",
			content:     "- record: foo\n  expr: sum(found_7 * on (job) sum(sum(notfound))) / found_7\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Bug, true, &rrSet),
		},
		{
			description: "complex query / bug but recording rule present",
			content:     "- record: foo\n  expr: sum(found_7 * on (job) sum(sum({foo=\"bar\"}))) / found_7\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Bug, true, &rrSet),
		},
		{
			description: "complex query / bug, recording rule present but setting off",
			content:     "- record: foo\n  expr: sum(found_7 * on (job) sum(sum(notfound))) / found_7\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Bug, false, &rrSet),
			problems: []checks.Problem{
				{
					Fragment: "notfound",
					Lines:    []int{2},
					Reporter: "query/series",
					Text:     "query using prom completed without any results for notfound",
					Severity: checks.Bug,
				},
			},
		},
		{
			description: "label_replace()",
			content: `- alert: foo
  expr: |
    count(
      label_replace(
        node_filesystem_readonly{mountpoint!=""},
        "device",
        "$2",
        "device",
        "/dev/(mapper/luks-)?(sd[a-z])[0-9]"
      )
    ) by (device,instance) > 0
    and on (device, instance)
    label_replace(
      disk_info{type="sat",interface_speed!="6.0 Gb/s"},
      "device",
      "$1",
      "disk",
      "/dev/(sd[a-z])"
    )
  for: 5m
`,
			checker: checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Bug, false, nil),
		},
		{
			description: "offset",
			content:     "- record: foo\n  expr: node_filesystem_readonly{mountpoint!=\"\"} offset 5m\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Bug, false, nil),
		},
		{
			description: "series found, label missing",
			content:     "- record: foo\n  expr: found{job=\"notfound\"}\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Warning, false, nil),
			problems: []checks.Problem{
				{
					Fragment: `found{job="notfound"}`,
					Lines:    []int{2},
					Reporter: "query/series",
					Text:     `query using prom completed without any results for found{job="notfound"}`,
					Severity: checks.Warning,
				},
			},
		},
		{
			description: "series missing, label missing",
			content:     "- record: foo\n  expr: notfound{job=\"notfound\"}\n",
			checker:     checks.NewSeriesCheck("prom", srv.URL, time.Second*5, checks.Warning, false, nil),
			problems: []checks.Problem{
				{
					Fragment: "notfound",
					Lines:    []int{2},
					Reporter: "query/series",
					Text:     "query using prom completed without any results for notfound",
					Severity: checks.Warning,
				},
			},
		},
	}
	runTests(t, testCases)
}
