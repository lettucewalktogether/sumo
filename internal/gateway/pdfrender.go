package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// chromeCandidates is the ordered list of paths and PATH names where
// a Chrome/Chromium binary commonly lives on macOS, Linux, and
// Windows. Returning the first hit means a typical install (Google
// Chrome.app on macOS, /usr/bin/chromium on Debian, etc.) needs no
// configuration to make PDF export work.
var chromeCandidates = []string{
	// macOS app bundles
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
	// PATH names
	"google-chrome",
	"google-chrome-stable",
	"chromium",
	"chromium-browser",
	"chrome",
	"microsoft-edge",
	"brave-browser",
}

// chromeBinaryPath returns the absolute path of a usable Chrome /
// Chromium / Edge / Brave binary, or a friendly install-hint error
// when none is found. Looks first at well-known macOS app-bundle
// paths, then falls back to PATH lookups.
func chromeBinaryPath() (string, error) {
	for _, c := range chromeCandidates {
		if c[0] == '/' {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c, nil
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("PDF export needs Chrome, Chromium, Edge, or Brave installed " +
		"(none found in /Applications/ or on PATH). Install Chrome from " +
		"https://www.google.com/chrome/ — or use the Markdown / Word export " +
		"options instead, which have no browser dependency.")
}

// pdfRenderTimeout caps a single render at 30 s. Generous given Chrome
// startup is typically 1–2 s and the page is a single self-contained
// HTML document — anything past 30 s indicates Chrome is wedged.
const pdfRenderTimeout = 30 * time.Second

// pdfRenderSem caps concurrent PDF renders at 2. Each render spins up
// its own Chrome process via chromedp.NewExecAllocator, which is
// memory-heavy (~150 MB resident); two in flight is the sweet spot
// between throughput and not OOM-ing a developer's laptop. Requests
// past the cap queue rather than fail.
var pdfRenderSem = make(chan struct{}, 2)

// pdfChromePathOnce caches the result of locating a Chrome/Chromium
// binary so repeated PDF renders don't repeat the lookup. Looked up
// lazily on first render.
var (
	pdfChromePathOnce sync.Once
	pdfChromePathVal  string
	pdfChromePathErr  error
)

// renderHTMLToPDF converts the supplied HTML document into a real
// PDF byte payload by piping it through a headless Chrome instance.
// Reuses the chromedp dependency Felix already pulls in for its
// browser tool — no new system dependency, no LaTeX, no
// wkhtmltopdf. Output is letter-size with 0.5" margins and
// PrintBackground=true so the styled <header> bar in the export
// HTML carries through to the PDF.
//
// The rendering is bounded:
//   - 30 s overall timeout
//   - up to 2 concurrent renders (others queue on pdfRenderSem)
//   - context cancellation tears down the chrome process cleanly
func renderHTMLToPDF(parentCtx context.Context, html string) ([]byte, error) {
	if err := ensureChromeAvailable(); err != nil {
		return nil, err
	}

	// Bound concurrency. Caller's context is honored while we wait
	// for a slot so a stalled queue can't pin a dropped client.
	select {
	case pdfRenderSem <- struct{}{}:
		defer func() { <-pdfRenderSem }()
	case <-parentCtx.Done():
		return nil, fmt.Errorf("pdf render queue: %w", parentCtx.Err())
	}

	ctx, cancel := context.WithTimeout(parentCtx, pdfRenderTimeout)
	defer cancel()

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx,
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.NoSandbox,
			chromedp.ExecPath(pdfChromePathVal),
			chromedp.Flag("hide-scrollbars", true),
			chromedp.Flag("disable-blink-features", "AutomationControlled"),
		)...,
	)
	defer allocCancel()
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	// data: URL avoids touching the filesystem. Base64-encoded so an
	// HTML document containing single quotes / # / & doesn't get
	// silently truncated by the URL parser.
	dataURL := "data:text/html;charset=utf-8;base64," +
		base64.StdEncoding.EncodeToString([]byte(html))

	var pdf []byte
	err := chromedp.Run(taskCtx,
		chromedp.Navigate(dataURL),
		// WaitReady ensures the DOM is built before we print so a
		// large response doesn't print mid-render with a half-empty
		// page.
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			buf, _, err := page.PrintToPDF().
				WithPrintBackground(true).
				WithPaperWidth(8.5).
				WithPaperHeight(11).
				WithMarginTop(0.5).
				WithMarginBottom(0.5).
				WithMarginLeft(0.5).
				WithMarginRight(0.5).
				WithPreferCSSPageSize(true).
				Do(ctx)
			if err != nil {
				return err
			}
			pdf = buf
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("chromedp pdf render: %w", err)
	}
	if len(pdf) == 0 {
		return nil, fmt.Errorf("chromedp pdf render: empty output")
	}
	return pdf, nil
}

// ensureChromeAvailable verifies a Chrome/Chromium binary is on the
// system before we kick off chromedp. Returns a friendly install
// hint when none is found rather than letting chromedp panic the
// gateway with an obscure exec error.
func ensureChromeAvailable() error {
	pdfChromePathOnce.Do(func() {
		pdfChromePathVal, pdfChromePathErr = chromeBinaryPath()
	})
	return pdfChromePathErr
}
