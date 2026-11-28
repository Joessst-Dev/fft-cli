package network

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"html/template"
)

//go:embed report/report.html.tmpl report/report.css report/report.js
var reportAssets embed.FS

var (
	reportTemplate = template.Must(template.ParseFS(reportAssets, "report/report.html.tmpl"))
	reportCSS      = mustAsset("report/report.css")
	reportJS       = mustAsset("report/report.js")
)

func mustAsset(name string) string {
	b, err := reportAssets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ReportCSP is the page's Content-Security-Policy: nothing may load, connect
// or submit anywhere, and the only script and style that run are the ones fft
// embedded, by hash.
//
// A report holds the tenant's network and is opened in a browser, where
// anything that slipped into a facility's name could try to run. The policy is
// the second wall behind the page writing tenant text only as text: even then,
// nothing in the page can send the data anywhere. It also lets whoever opens
// the file see for themselves that it works offline.
func ReportCSP() string {
	return "default-src 'none'; script-src " + cspHash(reportJS) + "; style-src " + cspHash(reportCSS) +
		"; img-src data:; base-uri 'none'; form-action 'none'"
}

func cspHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// RenderReport writes the view as one self-contained HTML page: the data, the
// script that draws it and its styles, inline. It needs no server and makes no
// request.
func RenderReport(v View) ([]byte, error) {
	title := v.Project
	if title == "" {
		title = "snapshot"
	}
	var b bytes.Buffer
	// The view is handed over as a value, not as encoded text: html/template
	// then encodes it as JSON for the script element itself, escaping '<', '>'
	// and '&', so no facility name can close the element and start markup.
	//
	// The script and styles go in unescaped, which is what they must be to run.
	// They are fft's own embedded files, fixed at build time, and the CSP admits
	// exactly them by hash; no tenant text is ever part of them.
	css := template.CSS(reportCSS) //nolint:gosec // fft's embedded stylesheet, not input
	js := template.JS(reportJS)    //nolint:gosec // fft's embedded script, not input
	err := reportTemplate.Execute(&b, struct {
		CSP, Title string
		CSS        template.CSS
		JS         template.JS
		View       View
	}{CSP: ReportCSP(), Title: title, CSS: css, JS: js, View: v})
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
