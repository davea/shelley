package lazycue

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// newBrowserOrSkip launches a headless browser, skipping the test cleanly when
// none is available (or when the integration env var is unset). Mirrors the
// gating used by the test/ package's LazyCue integration tests.
func newBrowserOrSkip(t *testing.T) *Browser {
	t.Helper()
	if os.Getenv("LAZYCUE_INTEGRATION") == "" {
		t.Skip("set LAZYCUE_INTEGRATION=1 to run LazyCue browser tests")
	}
	br, err := NewBrowser(context.Background())
	if err != nil {
		t.Skipf("no browser available: %v", err)
	}
	t.Cleanup(br.Close)
	return br
}

// serveHTML serves a fixed HTML document at / and returns the server URL.
func serveHTML(t *testing.T, html string) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

// TestAssertPollsUntilSettled proves the point-in-time assert_* steps now poll:
// a page that starts WITH a .spinner and removes it after ~600ms must pass
// `assert_count .spinner 0` (the old one-shot check failed here under load,
// which is the flake this change fixes). The spinner is present when the step
// first runs, so a non-polling assert would fail immediately.
func TestAssertPollsUntilSettled(t *testing.T) {
	br := newBrowserOrSkip(t)
	url := serveHTML(t, `<!doctype html><html><body>
<div class="spinner">loading</div>
<script>setTimeout(function(){document.querySelector('.spinner').remove();}, 600);</script>
</body></html>`)

	steps := []Step{
		{Action: ActionNavigate, URL: url},
		{Action: ActionAssertCount, Selector: ".spinner", Count: 0},
	}
	results, err := br.ExecuteSteps(context.Background(), url, steps)
	if err != nil {
		t.Fatalf("ExecuteSteps returned error: %v (results=%+v)", err, results)
	}
	for i, r := range results {
		if !r.Pass {
			t.Fatalf("step %d (%s) failed: %s", i, r.Action, r.Error)
		}
	}
}

// TestAssertStillFailsWhenNeverTrue proves polling doesn't mask real failures:
// a page whose .spinner never goes away must still fail `assert_count .spinner
// 0` after the (short, overridden) timeout. This is the genuine forever-spinner
// regression the test guards against — polling only tolerates LATE settling, it
// never turns a never-true assertion into a pass.
func TestAssertStillFailsWhenNeverTrue(t *testing.T) {
	br := newBrowserOrSkip(t)
	url := serveHTML(t, `<!doctype html><html><body>
<div class="spinner">loading forever</div>
</body></html>`)

	steps := []Step{
		{Action: ActionNavigate, URL: url},
		{Action: ActionAssertCount, Selector: ".spinner", Count: 0, Timeout: "1s"},
	}
	start := time.Now()
	results, err := br.ExecuteSteps(context.Background(), url, steps)
	if err == nil {
		t.Fatalf("expected assert_count to fail for a persistent spinner, got success: %+v", results)
	}
	// It should have polled for roughly the timeout before giving up, not
	// returned instantly (which would prove it wasn't really polling) and not
	// hung well past the deadline.
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > 6*time.Second {
		t.Fatalf("assert_count returned after %s, want ~1s (the polling timeout)", elapsed)
	}
}

// TestAssertVariantsPollUntilSettled exercises the polling mechanism across
// several assert_* variants (not just assert_count), each starting in the
// not-yet-satisfied state and becoming satisfied ~500ms later. Guards against a
// future edit to one case's closure quietly dropping the polling wrapper.
func TestAssertVariantsPollUntilSettled(t *testing.T) {
	br := newBrowserOrSkip(t)
	// The target starts hidden+empty; a timer reveals it and fills its text.
	url := serveHTML(t, `<!doctype html><html><body>
<div id="t" style="display:none"></div>
<script>setTimeout(function(){
  var el=document.getElementById('t');
  el.style.display='block';
  el.textContent='ready now';
}, 500);</script>
</body></html>`)

	cases := []struct {
		name string
		step Step
	}{
		{"assert_visible", Step{Action: ActionAssertVisible, Selector: "#t"}},
		{"assert_text", Step{Action: ActionAssertText, Selector: "#t", Text: "ready now"}},
		{"assert_text_contains", Step{Action: ActionAssertTextContains, Selector: "#t", Text: "ready"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps := []Step{
				{Action: ActionNavigate, URL: url},
				tc.step,
			}
			results, err := br.ExecuteSteps(context.Background(), url, steps)
			if err != nil {
				t.Fatalf("%s: ExecuteSteps error: %v (results=%+v)", tc.name, err, results)
			}
			for i, r := range results {
				if !r.Pass {
					t.Fatalf("%s: step %d (%s) failed: %s", tc.name, i, r.Action, r.Error)
				}
			}
		})
	}
}
