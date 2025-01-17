package checks_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudflare/pint/internal/checks"

	"github.com/google/go-cmp/cmp"
	"github.com/rs/zerolog"
)

func TestCostCheck(t *testing.T) {
	zerolog.SetGlobalLevel(zerolog.FatalLevel)

	content := "- record: foo\n  expr: sum(foo)\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			t.Fatal(err)
		}
		query := r.Form.Get("query")
		if query != "count(sum(foo))" {
			t.Fatalf("Prometheus got invalid query: %s", query)
		}

		switch r.URL.Path {
		case "/empty/api/v1/query":
			time.Sleep(time.Second * 2)
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[]
				}
			}`))
		case "/1/api/v1/query":
			time.Sleep(time.Millisecond * 550)
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[{"metric":{},"value":[1614859502.068,"1"]}]
				}
			}`))
		case "/7/api/v1/query":
			time.Sleep(time.Millisecond * 100)
			w.WriteHeader(200)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"resultType":"vector",
					"result":[{"metric":{},"value":[1614859502.068,"7"]}]
				}
			}`))
		default:
			w.WriteHeader(400)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"status":"error",
				"errorType":"bad_data",
				"error":"unhandled path"
			}`))
		}
	}))
	defer srv.Close()

	testCases := []checkTest{
		{
			description: "ignores rules with syntax errors",
			content:     "- record: foo\n  expr: sum(foo) without(\n",
			checker:     checks.NewCostCheck("prom", "http://localhost", time.Second*5, 4096, 0, checks.Bug),
		},
		{
			description: "empty response",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/empty/", time.Second*5, 4096, 0, checks.Bug),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     `RE:query using prom completed in 2\...s returning 0 result\(s\)`,
					Severity: checks.Information,
				},
			},
		},
		{
			description: "response timeout",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/empty/", time.Millisecond*5, 4096, 0, checks.Bug),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     `RE:query using prom failed with: Post "http://.+/empty/api/v1/query": context deadline exceeded`,
					Severity: checks.Bug,
				},
			},
		},
		{
			description: "bad request",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/400/", time.Second*5, 4096, 0, checks.Bug),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     "query using prom failed with: bad_data: unhandled path",
					Severity: checks.Bug,
				},
			},
		},
		{
			description: "1 result",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/1/", time.Second*5, 4096, 0, checks.Bug),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     `RE:query using prom completed in 0\...s returning 1 result\(s\) with 4\.0KiB estimated memory usage`,
					Severity: checks.Information,
				},
			},
		},
		{
			description: "7 results",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/7/", time.Second*5, 101, 0, checks.Bug),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     `RE:query using prom completed in 0\...s returning 7 result\(s\) with 707B estimated memory usage`,
					Severity: checks.Information,
				},
			},
		},
		{
			description: "7 result with MB",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/7/", time.Second*5, 1024*1024, 0, checks.Bug),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     `RE:query using prom completed in 0\...s returning 7 result\(s\) with 7\.0MiB estimated memory usage`,
					Severity: checks.Information,
				},
			},
		},
		{
			description: "7 results with 1 series max (1KB bps)",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/7/", time.Second*5, 1024, 1, checks.Bug),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     `RE:query using prom completed in 0\...s returning 7 result\(s\) with 7.0KiB estimated memory usage, maximum allowed series is 1`,
					Severity: checks.Bug,
				},
			},
		},
		{
			description: "7 results with 5 series max",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/7/", time.Second*5, 0, 5, checks.Bug),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     `RE:query using prom completed in 0\...s returning 7 result\(s\), maximum allowed series is 5`,
					Severity: checks.Bug,
				},
			},
		},
		{
			description: "7 results with 5 series max / infi",
			content:     content,
			checker:     checks.NewCostCheck("prom", srv.URL+"/7/", time.Second*5, 0, 5, checks.Information),
			problems: []checks.Problem{
				{
					Fragment: "sum(foo)",
					Lines:    []int{2},
					Reporter: "query/cost",
					Text:     `RE:query using prom completed in 0\...s returning 7 result\(s\), maximum allowed series is 5`,
					Severity: checks.Information,
				},
			},
		},
	}

	cmpText := cmp.Comparer(func(x, y string) bool {
		if strings.HasPrefix(x, "RE:") {
			xr := regexp.MustCompile(x[3:])
			return xr.MatchString(y)
		}
		return x == y
	})

	runTests(t, testCases, cmpText)
}
