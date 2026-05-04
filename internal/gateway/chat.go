package gateway

import (
	"fmt"
	"net/http"
	"strings"
)

// NewChatHandler returns an HTTP handler func that serves the chat web
// interface. The version string is injected into the sidebar brand so
// the running build is visible at a glance — sanitised through
// sanitizeVersionString first since it ends up in raw HTML.
func NewChatHandler(port int, version string) http.HandlerFunc {
	safeVersion := sanitizeVersionString(version)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// blob: in img-src is needed for the in-page attachment thumbnails
		// generated via URL.createObjectURL on dropped/picked image files.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src ws: wss:; img-src 'self' data: blob:")
		fmt.Fprintf(w, chatHTML, safeVersion, port)
	}
}

// sanitizeVersionString limits the version string to characters that
// are safe to drop directly into HTML without further escaping. git
// describe output is always within this set; a hostile build that
// somehow produces other characters gets them stripped rather than
// trusted into the page. Empty input falls back to "dev".
func sanitizeVersionString(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "dev"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '-', r == '_', r == '+':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "dev"
	}
	const maxLen = 40
	out := b.String()
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}

const chatHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Felix Chat</title>
<style>
:root {
	--bg: #1a1a2e;
	--bg-header: #16213e;
	--bg-msg-user: #0f3460;
	--bg-msg-asst: #16213e;
	--bg-code: #0d1b36;
	--bg-input: #0d1b36;
	--border: #0f3460;
	--text: #e0e0e0;
	--text-muted: #888;
	--text-strong: #fff;
	--text-em: #ccc;
	--accent: #16dbaa;
	--accent2: #53a8b6;
	--btn-text: #1a1a2e;
	--placeholder: #555;
	--error: #e74c3c;
	--tool-output: #aaa;
}
html.light {
	--bg: #f5f5f5;
	--bg-header: #ffffff;
	--bg-msg-user: #d1e7ff;
	--bg-msg-asst: #ffffff;
	--bg-code: #f0f0f0;
	--bg-input: #ffffff;
	--border: #ddd;
	--text: #1a1a1a;
	--text-muted: #777;
	--text-strong: #000;
	--text-em: #333;
	--accent: #0fa888;
	--accent2: #3a7f8c;
	--btn-text: #fff;
	--placeholder: #999;
	--error: #d32f2f;
	--tool-output: #555;
}
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, monospace;
	background: var(--bg);
	color: var(--text);
	height: 100vh;
	display: flex;
	flex-direction: column;
	transition: background 0.3s, color 0.3s;
}
#header {
	background: var(--bg-header);
	padding: 0.75rem 1.5rem;
	border-bottom: 1px solid var(--border);
	display: flex;
	align-items: center;
	gap: 0.75rem;
	flex-shrink: 0;
	transition: background 0.3s, border-color 0.3s;
}
#header .logo {
	width: 24px; height: 24px;
	filter: invert(1);
	transition: filter 0.3s;
}
html.light #header .logo {
	filter: none;
}
#header h1 { font-size: 1.1rem; color: var(--accent); }
#header .status { font-size: 0.8rem; color: var(--text-muted); }
#header .spacer { margin-left: auto; }
#theme-btn {
	background: none;
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.3rem 0.5rem;
	cursor: pointer;
	font-size: 1rem;
	line-height: 1;
	color: var(--text);
	transition: border-color 0.3s;
}
#theme-btn:hover, #clear-btn:hover, #toggle-tools-btn:hover, #agent-select:hover, #session-select:hover, #new-session-btn:hover { border-color: var(--accent); }
#toggle-tools-btn {
	background: none;
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.3rem 0.5rem;
	cursor: pointer;
	font-size: 0.8rem;
	line-height: 1;
	color: var(--text);
	transition: border-color 0.3s;
}
#toggle-tools-btn.active {
	border-color: var(--accent);
	color: var(--accent);
}
#toggle-trace-btn {
	background: none;
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.3rem 0.5rem;
	cursor: pointer;
	font-size: 0.8rem;
	line-height: 1;
	color: var(--text);
	transition: border-color 0.3s;
}
#toggle-trace-btn:hover { border-color: var(--accent); }
#toggle-trace-btn.active {
	border-color: var(--accent);
	color: var(--accent);
}
#trace-panel {
	border-top: 1px solid var(--border);
	background: var(--bg-msg-asst);
	max-height: 32vh;
	overflow-y: auto;
	font-family: "SF Mono", "Fira Code", monospace;
	font-size: 0.75rem;
	flex-shrink: 0;
}
#trace-header {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	padding: 0.4rem 1.5rem;
	border-bottom: 1px solid var(--border);
	color: var(--text-muted);
	background: var(--bg-header);
	position: sticky;
	top: 0;
}
#trace-title { font-weight: 600; color: var(--text); }
#trace-clear-btn {
	margin-left: auto;
	background: none;
	border: 1px solid var(--border);
	border-radius: 4px;
	padding: 0.15rem 0.5rem;
	cursor: pointer;
	font-size: 0.7rem;
	color: var(--text-muted);
}
#trace-clear-btn:hover { border-color: var(--accent); color: var(--accent); }
#trace-list {
	padding: 0.4rem 1.5rem;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}
.trace-row {
	display: grid;
	grid-template-columns: 5em 5em 1fr;
	gap: 0.6rem;
	color: var(--text-muted);
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
.trace-row .t-at { color: var(--text); text-align: right; }
.trace-row .t-dur { color: var(--accent2); text-align: right; }
.trace-row .t-phase { color: var(--text); }
.trace-row .t-attrs { color: var(--text-muted); }
.trace-row.slow .t-dur { color: var(--error); }
.trace-row.run-divider {
	color: var(--accent);
	border-top: 1px dashed var(--border);
	padding-top: 0.25rem;
	margin-top: 0.25rem;
}
#token-chip {
	font-size: 0.75rem;
	color: var(--text-muted);
	font-family: "SF Mono","Fira Code",monospace;
	padding: 0.2rem 0.5rem;
	border: 1px solid var(--border);
	border-radius: 12px;
	white-space: nowrap;
	cursor: help;
}
#token-chip.warn { border-color: var(--accent2); color: var(--accent2); }
#token-chip.danger { border-color: var(--error); color: var(--error); }
.mcp-reauth {
	margin: 0.5rem 0;
	padding: 0.5rem 0.75rem;
	background: var(--bg-msg-asst);
	border: 1px solid var(--accent2);
	border-radius: 6px;
	color: var(--accent2);
	font-size: 0.85rem;
	display: flex;
	align-items: center;
	gap: 0.5rem;
	flex-wrap: wrap;
}
.mcp-reauth.ok { border-color: var(--accent); color: var(--accent); }
.mcp-reauth.error { border-color: var(--error); color: var(--error); }
.mcp-reauth button {
	background: var(--accent2);
	color: white;
	border: none;
	padding: 0.3rem 0.7rem;
	border-radius: 4px;
	cursor: pointer;
	font-size: 0.85rem;
}
.mcp-reauth button:disabled { opacity: 0.6; cursor: wait; }
.mcp-reauth button:hover:not(:disabled) { filter: brightness(1.1); }
#bootstrap-banner {
	display: none;
	background: var(--bg-msg-asst);
	border-bottom: 1px solid var(--border);
	padding: 0.85rem 1.5rem;
	flex-shrink: 0;
}
#bootstrap-banner .bb-header {
	font-size: 0.85rem;
	color: var(--text);
	margin-bottom: 0.45rem;
	display: flex;
	gap: 0.5rem;
	align-items: baseline;
}
#bootstrap-banner .bb-title { font-weight: 600; color: var(--accent); }
#bootstrap-banner .bb-summary { color: var(--text-muted); }
#bootstrap-banner .bb-models {
	display: flex;
	flex-direction: column;
	gap: 0.4rem;
}
#bootstrap-banner .bb-row {
	display: grid;
	grid-template-columns: 12em 1fr 6em;
	gap: 0.6rem;
	font-size: 0.78rem;
	align-items: center;
}
#bootstrap-banner .bb-name { color: var(--text); font-family: "SF Mono","Fira Code",monospace; }
#bootstrap-banner .bb-status { color: var(--text-muted); text-align: right; font-variant-numeric: tabular-nums; }
#bootstrap-banner .bb-bar {
	height: 6px;
	background: var(--border);
	border-radius: 3px;
	overflow: hidden;
}
#bootstrap-banner .bb-fill {
	height: 100%%;
	background: var(--accent);
	width: 0%%;
	transition: width 0.4s ease;
}
#bootstrap-banner .bb-row.done .bb-fill { background: var(--accent2); }
#bootstrap-banner .bb-row.error .bb-fill { background: var(--error); }
#input-area.bootstrapping textarea { opacity: 0.5; pointer-events: none; }
#input-area.bootstrapping textarea::placeholder { color: var(--accent); }
#agent-select {
	background: var(--bg-input);
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.3rem 0.5rem;
	font-size: 0.85rem;
	color: var(--text);
	font-family: inherit;
	outline: none;
	cursor: pointer;
	transition: background 0.3s, border-color 0.3s, color 0.3s;
}
#agent-select:focus, #session-select:focus { border-color: var(--accent); }
#session-select {
	background: var(--bg-input);
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.3rem 0.5rem;
	font-size: 0.85rem;
	color: var(--text);
	font-family: inherit;
	outline: none;
	cursor: pointer;
	transition: background 0.3s, border-color 0.3s, color 0.3s;
}
#new-session-btn {
	background: none;
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.3rem 0.5rem;
	cursor: pointer;
	font-size: 0.8rem;
	line-height: 1;
	color: var(--text);
	transition: border-color 0.3s;
}
#clear-btn {
	background: none;
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.3rem 0.5rem;
	cursor: pointer;
	font-size: 0.8rem;
	line-height: 1;
	color: var(--text);
	transition: border-color 0.3s;
}
#messages {
	flex: 1;
	overflow-y: auto;
	padding: 1rem 1.5rem;
	display: flex;
	flex-direction: column;
	gap: 1rem;
}
.msg {
	position: relative;
	max-width: 85%%;
	padding: 0.75rem 1rem;
	border-radius: 12px;
	line-height: 1.5;
	font-size: 0.9rem;
	word-wrap: break-word;
	overflow-wrap: break-word;
	transition: background 0.3s, border-color 0.3s;
}
/* Per-bubble Export button. Top-right of the assistant message,
   hover-revealed (matches the per-row ⋮ menu pattern). The
   button has its own keyboard focus state so users on
   keyboard-only navigation can reach it without hovering. */
.msg-export-btn {
	position: absolute;
	top: 0.4rem;
	right: 0.4rem;
	width: 24px;
	height: 24px;
	display: flex;
	align-items: center;
	justify-content: center;
	background: transparent;
	border: 1px solid transparent;
	border-radius: 6px;
	color: var(--text-muted);
	cursor: pointer;
	opacity: 0;
	transition: opacity 0.15s ease, color 0.15s ease, border-color 0.15s ease, background 0.15s ease;
}
.msg.assistant:hover .msg-export-btn,
.msg-export-btn:focus-visible {
	opacity: 1;
}
.msg-export-btn:hover {
	color: var(--accent);
	border-color: var(--border);
	background: var(--bg-code);
}
.msg-export-btn svg { width: 14px; height: 14px; }
.msg.user {
	background: var(--bg-msg-user);
	align-self: flex-end;
	border-bottom-right-radius: 4px;
}
.msg.assistant {
	background: var(--bg-msg-asst);
	align-self: flex-start;
	border-bottom-left-radius: 4px;
	border: 1px solid var(--border);
}
.msg.assistant .content p { margin-bottom: 0.5em; }
.msg.assistant .content p:last-child { margin-bottom: 0; }
.msg.assistant .content code {
	background: var(--bg-code);
	padding: 0.15em 0.4em;
	border-radius: 3px;
	font-size: 0.9em;
	font-family: "SF Mono", "Fira Code", monospace;
}
.msg.assistant .content pre {
	background: var(--bg-code);
	padding: 0.75rem;
	border-radius: 6px;
	overflow-x: auto;
	margin: 0.5em 0;
	border: 1px solid var(--border);
	transition: background 0.3s, border-color 0.3s;
}
.msg.assistant .content pre code {
	background: none;
	padding: 0;
	font-size: 0.85em;
}
.msg.assistant .content a { color: var(--accent2); }
.msg.assistant .content strong { color: var(--text-strong); }
.msg.assistant .content em { color: var(--text-em); }
.msg.assistant .content h1,
.msg.assistant .content h2,
.msg.assistant .content h3,
.msg.assistant .content h4,
.msg.assistant .content h5,
.msg.assistant .content h6 {
	margin: 0.75em 0 0.25em;
	color: var(--text-strong);
}
.msg.assistant .content h1 { font-size: 1.4em; }
.msg.assistant .content h2 { font-size: 1.2em; }
.msg.assistant .content h3 { font-size: 1.05em; }
.msg.assistant .content hr {
	border: none;
	border-top: 1px solid var(--border);
	margin: 0.75em 0;
}
.msg.assistant .content ul, .msg.assistant .content ol {
	margin: 0.5em 0 0.5em 1.5em;
}
.msg.assistant .content li { margin-bottom: 0.25em; }
.msg.assistant .content table {
	border-collapse: collapse;
	margin: 0.5em 0;
	display: block;
	overflow-x: auto;
	max-width: 100%%;
}
.msg.assistant .content th,
.msg.assistant .content td {
	border: 1px solid var(--border);
	padding: 0.4em 0.75em;
	text-align: left;
}
.msg.assistant .content th {
	background: var(--bg-code);
	color: var(--text-strong);
	font-weight: 600;
}
.msg.assistant .content tr:nth-child(even) td {
	background: rgba(128,128,128,0.07);
}
.tool-call {
	background: var(--bg-code);
	border: 1px solid var(--border);
	border-radius: 6px;
	margin: 0.5rem 0;
	font-size: 0.85rem;
	max-width: 85%%;
	align-self: flex-start;
	transition: background 0.3s, border-color 0.3s;
}
.tool-call-header {
	padding: 0.4rem 0.75rem;
	color: var(--accent2);
	cursor: pointer;
	display: flex;
	align-items: center;
	gap: 0.5rem;
	user-select: none;
}
.tool-call-header .arrow {
	font-size: 0.7em;
	transition: transform 0.2s;
}
.tool-call-header .arrow.open { transform: rotate(90deg); }
.tool-call-output {
	display: none;
	padding: 0.5rem 0.75rem;
	border-top: 1px solid var(--border);
	color: var(--tool-output);
	white-space: pre-wrap;
	max-height: 300px;
	overflow-y: auto;
	font-family: "SF Mono", "Fira Code", monospace;
	font-size: 0.8rem;
}
.tool-call-output.show { display: block; }
.tool-call-output.error { color: var(--error); }
.tool-call-output img {
	display: block;
	max-width: 100%%;
	max-height: 500px;
	border-radius: 6px;
	margin-top: 0.5rem;
	cursor: pointer;
}
.tool-call-output.has-image { max-height: none; }
.hide-tools .tool-call { display: none; }
.tool-call-header .tool-detail {
	color: var(--text-muted);
	font-family: "SF Mono", "Fira Code", monospace;
	font-size: 0.9em;
	max-width: 500px;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	display: inline-block;
	vertical-align: bottom;
}
#input-area {
	background: var(--bg-header);
	padding: 0.75rem 1.5rem;
	border-top: 1px solid var(--border);
	display: flex;
	gap: 0.75rem;
	flex-shrink: 0;
	transition: background 0.3s, border-color 0.3s;
}
#input {
	flex: 1;
	background: var(--bg-input);
	border: 1px solid var(--border);
	border-radius: 8px;
	padding: 0.6rem 1rem;
	color: var(--text);
	font-size: 0.95rem;
	font-family: inherit;
	outline: none;
	resize: none;
	min-height: 40px;
	max-height: 150px;
	transition: background 0.3s, border-color 0.3s, color 0.3s;
}
#input:focus { border-color: var(--accent); }
#input::placeholder { color: var(--placeholder); }
#send-btn {
	background: var(--accent);
	color: var(--btn-text);
	border: none;
	border-radius: 8px;
	padding: 0 1.25rem;
	font-size: 0.95rem;
	font-weight: 600;
	cursor: pointer;
	transition: opacity 0.2s, background 0.3s;
	align-self: flex-end;
	height: 40px;
}
#send-btn:hover { opacity: 0.85; }
#send-btn:disabled { opacity: 0.4; cursor: not-allowed; }
#stop-btn {
	background: var(--error);
	color: #fff;
	border: none;
	border-radius: 8px;
	padding: 0 1.25rem;
	font-size: 0.95rem;
	font-weight: 600;
	cursor: pointer;
	transition: opacity 0.2s, background 0.3s;
	align-self: flex-end;
	height: 40px;
	display: none;
}
#stop-btn:hover { opacity: 0.85; }

/* Attachment UI — chip strip above the textarea, attach button next to send. */
#input-area-wrap {
	background: var(--bg-header);
	border-top: 1px solid var(--border);
	padding: 0.75rem 1.5rem;
	flex-shrink: 0;
	transition: background 0.3s, border-color 0.3s;
	position: relative;
}
#input-area-wrap.drag-over {
	background: color-mix(in srgb, var(--accent) 15%%, var(--bg-header));
}
#input-area-wrap.drag-over::after {
	content: "Drop images to attach";
	position: absolute;
	inset: 0.75rem 1.5rem;
	border: 2px dashed var(--accent);
	border-radius: 10px;
	display: flex;
	align-items: center;
	justify-content: center;
	color: var(--accent);
	font-size: 0.95rem;
	font-weight: 600;
	pointer-events: none;
	background: color-mix(in srgb, var(--bg-header) 80%%, transparent);
}
#attachment-strip {
	display: flex;
	flex-wrap: wrap;
	gap: 0.5rem;
	margin-bottom: 0.5rem;
}
#attachment-strip:empty { display: none; }
.attachment-chip {
	display: inline-flex;
	align-items: center;
	gap: 0.5rem;
	background: var(--bg-input);
	border: 1px solid var(--border);
	border-radius: 8px;
	padding: 0.25rem 0.5rem 0.25rem 0.25rem;
	max-width: 220px;
	font-size: 0.8rem;
	color: var(--text-em);
}
.attachment-thumb {
	width: 32px; height: 32px;
	border-radius: 4px;
	object-fit: cover;
	flex-shrink: 0;
	background: var(--bg);
}
.attachment-glyph {
	width: 32px; height: 32px;
	border-radius: 4px;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	font-size: 0.6rem;
	font-weight: 700;
	letter-spacing: 0.03em;
	flex-shrink: 0;
	background: var(--bg);
	color: var(--accent2);
	border: 1px solid var(--border);
	text-transform: uppercase;
}
.attachment-glyph[data-kind="doc"]  { color: var(--accent); }
.attachment-glyph[data-kind="text"] { color: var(--text-em); }
.attachment-name {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	flex: 1;
}
.attachment-size {
	color: var(--text-muted);
	font-size: 0.7rem;
	flex-shrink: 0;
}
.attachment-remove {
	background: none;
	border: none;
	color: var(--text-muted);
	cursor: pointer;
	font-size: 1rem;
	line-height: 1;
	padding: 0 0.15rem;
	flex-shrink: 0;
}
.attachment-remove:hover { color: var(--error); }
#input-area {
	background: transparent;
	padding: 0;
	border-top: none;
}
#attach-btn {
	background: var(--bg-input);
	color: var(--text-em);
	border: 1px solid var(--border);
	border-radius: 8px;
	width: 40px;
	height: 40px;
	padding: 0;
	font-size: 1.4rem;
	font-weight: 400;
	line-height: 1;
	cursor: pointer;
	transition: background 0.2s, border-color 0.2s, color 0.2s;
	align-self: flex-end;
	display: flex;
	align-items: center;
	justify-content: center;
}
#attach-btn:hover { border-color: var(--accent); color: var(--accent); }
#attach-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.msg .user-attachments {
	display: flex;
	flex-wrap: wrap;
	gap: 0.4rem;
	margin-top: 0.4rem;
}
.msg .user-attachments img {
	max-width: 220px;
	max-height: 180px;
	border-radius: 6px;
	border: 1px solid var(--border);
	display: block;
}
.user-attachment-pill {
	display: inline-block;
	padding: 0.2rem 0.55rem;
	border-radius: 999px;
	background: var(--bg-input);
	border: 1px solid var(--border);
	font-size: 0.75rem;
	color: var(--text-em);
}
.user-attachment-pill[data-kind="doc"]  { color: var(--accent); }
.user-attachment-pill[data-kind="text"] { color: var(--text-em); }
#attach-error {
	color: var(--error);
	font-size: 0.75rem;
	margin-bottom: 0.4rem;
	min-height: 0;
	transition: min-height 0.15s;
}
#attach-error:empty { display: none; }

/* ============================================================
   Sidebar + main layout — sessions on the left like ChatGPT /
   Claude.ai. Header lives inside #main-pane and decompresses to
   per-conversation controls only.
   ============================================================ */
#layout {
	display: flex;
	flex-direction: row;
	flex: 1;
	min-height: 0;
}
#sidebar {
	width: 260px;
	flex-shrink: 0;
	background: var(--bg-header);
	border-right: 1px solid var(--border);
	display: flex;
	flex-direction: column;
	min-height: 0;
	transition: width 0.18s ease, border-right-color 0.3s, background 0.3s;
}
#sidebar.collapsed { width: 0; border-right-width: 0; }
#sidebar.collapsed > * { display: none; }
#sidebar-header {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	padding: 0.75rem 0.9rem;
	border-bottom: 1px solid var(--border);
	flex-shrink: 0;
}
#sidebar-brand {
	font-size: 1.05rem;
	font-weight: 600;
	color: var(--accent);
	flex: 1;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
#sidebar-version {
	font-size: 0.62rem;
	font-weight: 400;
	color: var(--text-muted);
	font-family: "SF Mono", "Fira Code", monospace;
	letter-spacing: 0.01em;
	margin-left: 0.3rem;
	vertical-align: middle;
}
#new-chat-btn {
	display: flex;
	align-items: center;
	justify-content: center;
	gap: 0.45rem;
	margin: 0.6rem 0.6rem 0.4rem;
	padding: 0.55rem 0.8rem;
	background: var(--bg-input);
	color: var(--text);
	border: 1px solid var(--border);
	border-radius: 8px;
	font-size: 0.85rem;
	font-weight: 500;
	cursor: pointer;
	transition: border-color 0.2s, color 0.2s, background 0.2s;
}
#new-chat-btn:hover { border-color: var(--accent); color: var(--accent); }
#new-chat-btn .plus { font-size: 1.05rem; line-height: 1; }
#session-list {
	flex: 1;
	overflow-y: auto;
	padding: 0.2rem 0.4rem 0.6rem;
	min-height: 0;
}
#session-list::-webkit-scrollbar { width: 6px; }
#session-list::-webkit-scrollbar-thumb { background: var(--border); border-radius: 3px; }
.session-group-label {
	font-size: 0.65rem;
	font-weight: 600;
	letter-spacing: 0.08em;
	text-transform: uppercase;
	color: var(--text-muted);
	padding: 0.7rem 0.6rem 0.25rem;
}
/* Stale .session-row + .session-meta block from the PR 2 sidebar
   redesign was here. The new structure (div role=button + row-line +
   row-title + row-preview) supersedes it; the old overflow:hidden in
   particular was clipping the absolutely-positioned row context menu.
   Replacement rule lives further down with the rest of the row UI. */
#session-list-empty {
	color: var(--text-muted);
	font-size: 0.8rem;
	padding: 0.7rem 0.6rem;
	font-style: italic;
}
#sidebar-footer {
	padding: 0.5rem 0.7rem 0.6rem;
	border-top: 1px solid var(--border);
	flex-shrink: 0;
}
#sidebar-footer #token-chip {
	display: block;
	width: 100%%;
	box-sizing: border-box;
	font-size: 0.72rem;
	text-align: center;
	padding: 0.3rem 0.5rem;
	cursor: help;
}
#sidebar-toggle {
	background: none;
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.3rem 0.5rem;
	cursor: pointer;
	font-size: 1rem;
	line-height: 1;
	color: var(--text);
	transition: border-color 0.3s;
}
#sidebar-toggle:hover { border-color: var(--accent); }
#main-pane {
	flex: 1;
	display: flex;
	flex-direction: column;
	min-width: 0;
	min-height: 0;
}
/* The main pane needs an explicit min-height:0 too so flex-shrinking
   the messages area works on Safari (which otherwise refuses to
   shrink below content size). */
#messages-empty {
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	height: 100%%;
	color: var(--text-muted);
	font-size: 0.95rem;
	text-align: center;
	padding: 2rem;
	gap: 0.5rem;
}
#messages-empty .empty-title {
	font-size: 1.1rem;
	font-weight: 600;
	color: var(--text);
}
#messages-empty .empty-hint {
	font-size: 0.85rem;
	max-width: 28rem;
	line-height: 1.4;
}
#messages.has-messages #messages-empty { display: none; }
/* Connection-status pill — colored dot + label, replaces the
   plain "disconnected" text. */
#conn-status {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	font-size: 0.72rem;
	color: var(--text-muted);
	padding: 0.2rem 0.5rem;
	border: 1px solid var(--border);
	border-radius: 999px;
	white-space: nowrap;
}
#conn-status::before {
	content: "";
	display: inline-block;
	width: 7px; height: 7px;
	border-radius: 50%%;
	background: var(--text-muted);
}
#conn-status.connected { color: var(--accent); border-color: var(--accent); }
#conn-status.connected::before { background: var(--accent); }
#conn-status.error { color: var(--error); border-color: var(--error); }
#conn-status.error::before { background: var(--error); }
#conn-status.connecting::before { animation: pulse 1.4s ease-in-out infinite; }
@keyframes pulse {
	0%%, 100%% { opacity: 0.4; }
	50%%       { opacity: 1.0; }
}
/* Settings cog in the sidebar header — sits next to the brand. */
#settings-btn {
	background: none;
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 4px 6px;
	color: var(--text);
	cursor: pointer;
	display: inline-flex;
	align-items: center;
	justify-content: center;
}
#settings-btn:hover { border-color: var(--accent); color: var(--accent); }

/* Sidebar tab toggle (Threads / Jobs). */
#sidebar-tabs {
	display: flex;
	margin: 0.5rem 0.6rem 0;
	background: var(--bg-input);
	border-radius: 8px;
	padding: 3px;
	flex-shrink: 0;
}
.sidebar-tab {
	flex: 1;
	background: none;
	border: none;
	padding: 0.4rem 0.7rem;
	color: var(--text-muted);
	border-radius: 6px;
	font-size: 0.8rem;
	font-weight: 500;
	font-family: inherit;
	cursor: pointer;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: 0.35rem;
}
.sidebar-tab.active { background: var(--bg-header); color: var(--accent); }
.tab-count {
	background: var(--accent);
	color: var(--btn-text);
	border-radius: 999px;
	padding: 0 0.45rem;
	font-size: 0.65rem;
	font-weight: 700;
}
.tab-pane {
	display: flex;
	flex-direction: column;
	flex: 1;
	min-height: 0;
}
.tab-pane[hidden] { display: none; }

/* Inline SVG icon helper — every icon is 14x14 currentColor. */
.icon {
	width: 14px;
	height: 14px;
	flex-shrink: 0;
	display: inline-block;
	vertical-align: middle;
}

/* Replace the old session-row buttons with a div role=button so the
   row can legally contain a flex line + a nested ⋮ button. The CSS
   targets .session-row everywhere — same hooks the JS already uses. */
.session-row {
	position: relative;
	display: block;
	border-radius: 6px;
	padding: 0.5rem 0.6rem 0.55rem;
	color: var(--text-em);
	font-size: 0.88rem;
	cursor: pointer;
	margin-bottom: 1px;
}
.session-row:hover { background: var(--bg-input); color: var(--text); }
.session-row.active { background: var(--bg-input); color: var(--accent); }
.session-row .row-line {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	min-width: 0;
}
.session-row .row-title {
	flex: 1;
	min-width: 0;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
.session-row .row-preview {
	display: block;
	font-size: 0.75rem;
	color: var(--text-muted);
	margin-top: 0.15rem;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
/* ⋮ button sits inline at the end of the title line — hover-only. */
.session-row .row-more {
	flex-shrink: 0;
	background: transparent;
	border: none;
	color: inherit;
	cursor: pointer;
	width: 22px;
	height: 22px;
	border-radius: 4px;
	padding: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	opacity: 0;
	transition: opacity 0.1s ease;
}
.session-row:hover .row-more,
.session-row.show-menu .row-more { opacity: 1; }
.session-row .row-more:hover { background: var(--bg-header); color: var(--accent); }

/* Per-row context menu popover (Rename / Pin / Delete). The slightly
   brighter background + accent2 border lifts the menu visually off
   the sidebar's bg-header, which would otherwise blend (both dark
   navy). The drop shadow gives a clear "this floats above" cue. */
/* Export dialog — modal overlay anchored to the body, dismissible
   on backdrop click or Escape. The card centres in the viewport
   with a soft shadow; format buttons are a flex-wrapped grid so
   the layout adapts to whatever buttons fit on a row. */
.export-overlay {
	position: fixed;
	inset: 0;
	background: rgba(0,0,0,0.55);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 100;
}
.export-card {
	background: var(--bg-header);
	border: 1px solid var(--border);
	border-radius: 12px;
	padding: 1.25rem 1.4rem;
	min-width: 380px;
	max-width: 480px;
	box-shadow: 0 16px 48px rgba(0,0,0,0.5);
	color: var(--text);
}
.export-card h3 {
	font-size: 1rem;
	font-weight: 600;
	color: var(--text-strong);
	margin-bottom: 0.2rem;
}
.export-card .export-sub {
	font-size: 0.78rem;
	color: var(--text-muted);
	margin-bottom: 1rem;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.export-formats {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: 0.5rem;
	margin-bottom: 1rem;
}
.export-fmt {
	background: var(--bg-input);
	border: 1px solid var(--border);
	border-radius: 8px;
	color: var(--text);
	padding: 0.55rem 0.6rem;
	font-size: 0.85rem;
	font-family: inherit;
	cursor: pointer;
	transition: border-color 0.15s, color 0.15s;
}
.export-fmt:hover { border-color: var(--accent); color: var(--accent); }
.export-toggle {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	font-size: 0.85rem;
	color: var(--text-em);
	cursor: pointer;
	margin-bottom: 1rem;
}
.export-toggle input { cursor: pointer; }
.export-actions {
	display: flex;
	justify-content: flex-end;
}
.export-cancel-btn {
	background: transparent;
	border: 1px solid var(--border);
	border-radius: 6px;
	color: var(--text);
	padding: 0.4rem 0.9rem;
	font-size: 0.85rem;
	font-family: inherit;
	cursor: pointer;
}
.export-cancel-btn:hover { border-color: var(--accent); color: var(--accent); }

.row-menu {
	position: absolute;
	right: 0.4rem;
	top: 100%%;
	transform: translateY(-2px);
	background: var(--bg-msg-user);
	border: 1px solid var(--accent2);
	border-radius: 8px;
	box-shadow: 0 8px 24px rgba(0,0,0,0.55);
	padding: 0.3rem;
	min-width: 168px;
	z-index: 10;
	font-size: 0.82rem;
}
.row-menu button {
	display: flex;
	align-items: center;
	gap: 0.55rem;
	width: 100%%;
	background: none;
	border: none;
	padding: 0.4rem 0.55rem;
	color: var(--text);
	border-radius: 4px;
	cursor: pointer;
	text-align: left;
	font-family: inherit;
	font-size: 0.82rem;
}
.row-menu button:hover { background: var(--bg-input); }
.row-menu .menu-sep { height: 1px; background: var(--border); margin: 0.2rem 0.1rem; }
.row-menu .danger { color: var(--error); }
.row-menu .danger:hover { background: rgba(231,76,60,0.12); }

/* Inline rename: title cell becomes an editable input. */
.session-row.editing .row-title { display: none; }
.session-row.editing .row-rename-input {
	flex: 1;
	min-width: 0;
	background: var(--bg);
	border: 1px solid var(--accent);
	border-radius: 4px;
	color: var(--text);
	font-size: 0.83rem;
	padding: 0.2rem 0.4rem;
	font-family: inherit;
}

/* Pinned badge on a session row. */
.session-row .row-pin-mark {
	flex-shrink: 0;
	color: var(--accent2);
	display: inline-flex;
	align-items: center;
}
.session-row .row-pin-mark svg { width: 11px; height: 11px; }

/* Job rows (Step 3 fills these in; basic styling here so the
   placeholder list looks tidy). */
#jobs-list, #session-list { flex: 1; overflow-y: auto; padding: 0 0.4rem 0.6rem; }
#jobs-list-empty, #session-list-empty { color: var(--text-muted); font-size: 0.8rem; padding: 0.7rem 0.6rem; font-style: italic; }
.job-row {
	position: relative;
	background: var(--bg-input);
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.55rem 0.6rem;
	margin: 0 0.2rem 0.4rem;
	font-size: 0.82rem;
	color: var(--text-em);
	cursor: pointer;
}
.job-row:hover { border-color: var(--accent); }
.job-row.active { border-color: var(--accent); }
.job-row .job-title { display: block; color: var(--text); padding-right: 6.5rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.job-row .job-state {
	display: inline-flex; align-items: center; gap: 0.3rem;
	font-size: 0.7rem; color: var(--accent);
	margin-top: 0.25rem;
}
.job-row .job-state::before {
	content: ""; width: 6px; height: 6px; border-radius: 50%%;
	background: var(--accent);
	animation: jobpulse 1.4s ease-in-out infinite;
}
.job-row .job-state.paused { color: var(--text-muted); }
.job-row .job-state.paused::before { background: var(--text-muted); animation: none; }
.job-row .job-state.error { color: var(--error); }
.job-row .job-state.error::before { background: var(--error); animation: none; }
.job-row.error { border-color: rgba(231,76,60,0.45); }
.job-row .job-meta { color: var(--text-muted); font-size: 0.7rem; margin-top: 0.15rem; }
@keyframes jobpulse { 0%%,100%%{opacity:0.4} 50%%{opacity:1} }
.job-actions {
	position: absolute;
	right: 0.4rem; top: 0.4rem;
	display: none;
	gap: 2px;
	background: var(--bg);
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 2px;
}
.job-row:hover .job-actions { display: inline-flex; }
.job-actions button {
	background: none; border: none;
	color: var(--text-em); cursor: pointer;
	width: 26px; height: 26px;
	border-radius: 4px; padding: 0;
	display: inline-flex; align-items: center; justify-content: center;
}
.job-actions button:hover { background: var(--bg-input); color: var(--accent); }

/* Bubble bidi: dir=auto is added on the elements (set in JS), and
   unicode-bidi:plaintext makes the browser resolve direction per
   paragraph from the first strong character. RTL Arabic / Hebrew
   lines flow correctly without flipping LTR English. */
.msg.user, .msg.assistant .content {
	unicode-bidi: plaintext;
}

/* Job detail card — replaces the messages/input bar in the main
   pane when the user clicks a job in the Jobs sidebar tab. Keeps
   the same outer layout so toggling back is just a display swap. */
#job-detail {
	flex: 1;
	overflow-y: auto;
	padding: 1.25rem 1.75rem 2rem;
	display: flex;
	flex-direction: column;
	gap: 0.85rem;
}
.job-detail-head {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	border-bottom: 1px solid var(--border);
	padding-bottom: 0.6rem;
	margin-bottom: 0.4rem;
}
.job-detail-back {
	background: var(--bg-input);
	border: 1px solid var(--border);
	border-radius: 6px;
	padding: 0.35rem 0.7rem;
	color: var(--text);
	cursor: pointer;
	font-size: 0.8rem;
	font-family: inherit;
}
.job-detail-back:hover { border-color: var(--accent); color: var(--accent); }
.job-detail-title { font-size: 1.1rem; font-weight: 600; color: var(--text-strong); }
.job-detail-status { padding: 0.2rem 0; }
.job-detail-status .job-state { font-size: 0.85rem; }
.job-detail-field {
	display: grid;
	grid-template-columns: 7em 1fr;
	gap: 0.6rem 1rem;
	padding: 0.4rem 0;
	border-bottom: 1px dashed var(--border);
}
.job-detail-label {
	font-size: 0.75rem;
	color: var(--text-muted);
	letter-spacing: 0.05em;
	text-transform: uppercase;
}
.job-detail-value {
	color: var(--text);
	white-space: pre-wrap;
	word-break: break-word;
	font-family: "SF Mono", "Fira Code", monospace;
	font-size: 0.85rem;
}
.job-detail-actions {
	display: flex;
	gap: 0.5rem;
	flex-wrap: wrap;
	margin-top: 0.6rem;
}
.job-detail-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.45rem;
	padding: 0.5rem 0.9rem;
	background: var(--bg-input);
	border: 1px solid var(--border);
	border-radius: 8px;
	color: var(--text);
	cursor: pointer;
	font-size: 0.85rem;
	font-family: inherit;
}
.job-detail-btn:hover { border-color: var(--accent); color: var(--accent); }
.job-detail-btn.danger:hover { border-color: var(--error); color: var(--error); }
.job-detail-hint {
	margin-top: 1.2rem;
	color: var(--text-muted);
	font-size: 0.78rem;
	border-left: 2px solid var(--border);
	padding-left: 0.6rem;
	max-width: 60ch;
}

/* Narrow viewport: collapse the sidebar by default; hamburger
   toggle in the header reveals it as an overlay rather than
   shrinking the main pane. */
@media (max-width: 700px) {
	#sidebar:not(.open) { width: 0; border-right-width: 0; }
	#sidebar:not(.open) > * { display: none; }
	#sidebar.open {
		position: absolute;
		top: 0; bottom: 0; left: 0;
		width: 80vw;
		max-width: 320px;
		z-index: 50;
		box-shadow: 0 0 24px rgba(0,0,0,0.3);
	}
}
</style>
</head>
<body>
<svg xmlns="http://www.w3.org/2000/svg" style="display:none">
<defs>
	<symbol id="i-more" viewBox="0 0 24 24" fill="currentColor">
		<circle cx="12" cy="5" r="1.7"/>
		<circle cx="12" cy="12" r="1.7"/>
		<circle cx="12" cy="19" r="1.7"/>
	</symbol>
	<symbol id="i-cog" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
		<circle cx="12" cy="12" r="3"/>
		<path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
	</symbol>
	<symbol id="i-pencil" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
		<path d="M12 20h9"/>
		<path d="M16.5 3.5a2.121 2.121 0 1 1 3 3L7 19l-4 1 1-4z"/>
	</symbol>
	<symbol id="i-pin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
		<line x1="12" y1="17" x2="12" y2="22"/>
		<path d="M5 17h14l-2-7V4H7v6z"/>
	</symbol>
	<symbol id="i-trash" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
		<polyline points="3 6 5 6 21 6"/>
		<path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/>
		<path d="M10 11v6M14 11v6"/>
		<path d="M9 6V4a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2"/>
	</symbol>
	<symbol id="i-export" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
		<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
		<polyline points="7 10 12 5 17 10"/>
		<line x1="12" y1="5" x2="12" y2="15"/>
	</symbol>
	<symbol id="i-pause" viewBox="0 0 24 24" fill="currentColor">
		<rect x="7" y="5" width="3.5" height="14" rx="0.6"/>
		<rect x="13.5" y="5" width="3.5" height="14" rx="0.6"/>
	</symbol>
	<symbol id="i-play" viewBox="0 0 24 24" fill="currentColor">
		<path d="M7.5 4.5 L19 12 L7.5 19.5 Z"/>
	</symbol>
	<symbol id="i-rerun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
		<polyline points="20 4 20 9 15 9"/>
		<path d="M20 9a8 8 0 1 0-2 7"/>
	</symbol>
	<symbol id="i-log" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
		<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
		<polyline points="14 2 14 8 20 8"/>
		<line x1="8" y1="13" x2="16" y2="13"/>
		<line x1="8" y1="17" x2="16" y2="17"/>
	</symbol>
</defs>
</svg>
<div id="layout">
<aside id="sidebar">
	<div id="sidebar-header">
		<span id="sidebar-brand">Felix <span id="sidebar-version" title="Felix gateway version">%s</span></span>
		<button id="settings-btn" type="button" title="Open settings" aria-label="Open settings"><svg class="icon"><use href="#i-cog"/></svg></button>
	</div>
	<div id="sidebar-tabs" role="tablist">
		<button id="tab-threads-btn" class="sidebar-tab active" role="tab" aria-selected="true">Threads</button>
		<button id="tab-jobs-btn" class="sidebar-tab" role="tab" aria-selected="false">Jobs <span id="jobs-count" class="tab-count" hidden>0</span></button>
	</div>
	<div id="tab-threads" class="tab-pane active" role="tabpanel">
		<button id="new-chat-btn" type="button" title="Start a new conversation"><span class="plus">+</span> New chat</button>
		<div id="session-list" aria-label="Conversations">
			<div id="session-list-empty">No conversations yet.</div>
		</div>
	</div>
	<div id="tab-jobs" class="tab-pane" role="tabpanel" hidden>
		<div id="jobs-list" aria-label="Background jobs">
			<div id="jobs-list-empty">No background jobs.</div>
		</div>
	</div>
	<div id="sidebar-footer">
		<span id="token-chip" title="Tokens used / context window">—</span>
	</div>
</aside>
<main id="main-pane">
<div id="header">
	<button id="sidebar-toggle" type="button" title="Toggle sidebar" aria-label="Toggle sidebar">&#9776;</button>
	<select id="agent-select" title="Select agent"></select>
	<span class="spacer"></span>
	<button id="toggle-tools-btn" title="Hide/show tool calls">Tools</button>
	<button id="toggle-trace-btn" title="Hide/show live trace panel">Trace</button>
	<button id="clear-btn" title="Clear session">Clear</button>
	<button id="theme-btn" title="Toggle light/dark mode" aria-label="Toggle theme">&#9790;</button>
	<span id="conn-status" class="connecting">connecting</span>
</div>
<div id="bootstrap-banner">
	<div class="bb-header">
		<span class="bb-title">Setting up your local AI</span>
		<span class="bb-summary" id="bb-summary"></span>
	</div>
	<div class="bb-models" id="bb-models"></div>
</div>
<div id="messages">
	<div id="messages-empty">
		<div class="empty-title">Start a conversation</div>
		<div class="empty-hint">Type a message below, drop a file onto the window, or paste an image from the clipboard. Conversations save automatically and appear in the sidebar.</div>
	</div>
</div>
<div id="trace-panel" style="display:none;">
	<div id="trace-header"><span id="trace-title">Live trace</span><button id="trace-clear-btn" title="Clear trace">clear</button></div>
	<div id="trace-list"></div>
</div>
<div id="input-area-wrap">
	<div id="attach-error" aria-live="polite"></div>
	<div id="attachment-strip" aria-label="Attached files"></div>
	<div id="input-area">
		<button id="attach-btn" type="button" title="Attach an image, document, or text file" aria-label="Attach file">+</button>
		<input id="file-picker" type="file" accept="image/jpeg,image/png,image/gif,image/webp,image/bmp,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document,text/*,application/json,application/xml,application/x-yaml,application/yaml,application/javascript,application/x-sh,audio/mpeg,audio/mp4,audio/wav,audio/x-wav,audio/webm,audio/ogg,audio/flac,audio/aac,audio/x-m4a,.md,.txt,.csv,.json,.yaml,.yml,.xml,.html,.css,.js,.ts,.go,.py,.rb,.rs,.toml,.sql,.sh,.log,.mp3,.m4a,.wav,.webm,.ogg,.oga,.flac,.aac" multiple style="display:none">
		<textarea id="input" rows="1" placeholder="Type a message or drop a file..." dir="auto" lang="" autocapitalize="off" autocorrect="off" spellcheck="true" autofocus></textarea>
		<button id="send-btn" disabled>Send</button>
		<button id="stop-btn">Stop</button>
	</div>
</div>
</main>
</div>

<script>
(function() {
	var PORT = %d;
	var wsProto = (location.protocol === 'https:') ? 'wss://' : 'ws://';
	var wsBase = wsProto + location.host + location.pathname.replace(/\/chat\/?$/, '');
	var messagesEl = document.getElementById('messages');
	var inputEl = document.getElementById('input');
	var sendBtn = document.getElementById('send-btn');
	var connStatus = document.getElementById('conn-status');
	var themeBtn = document.getElementById('theme-btn');
	var clearBtn = document.getElementById('clear-btn');
	var stopBtn = document.getElementById('stop-btn');
	var agentSelect = document.getElementById('agent-select');
	var newChatBtn = document.getElementById('new-chat-btn');
	var sessionListEl = document.getElementById('session-list');
	var sessionListEmptyEl = document.getElementById('session-list-empty');
	var sidebarEl = document.getElementById('sidebar');
	var sidebarToggleBtn = document.getElementById('sidebar-toggle');
	var settingsBtn = document.getElementById('settings-btn');
	var tabThreadsBtn = document.getElementById('tab-threads-btn');
	var tabJobsBtn = document.getElementById('tab-jobs-btn');
	var tabThreadsPane = document.getElementById('tab-threads');
	var tabJobsPane = document.getElementById('tab-jobs');
	var jobsListEl = document.getElementById('jobs-list');
	var jobsListEmptyEl = document.getElementById('jobs-list-empty');
	var jobsCountEl = document.getElementById('jobs-count');

	// Active conversation tracking — the session-select dropdown went
	// away with the sidebar redesign, so we keep the active session key
	// here in JS (kept in lock-step with what the server thinks via
	// session.switch) and read it from the rendered .active row.
	var activeSessionKey = '';

	// Main pane mode. 'chat' → conversation messages + input bar (the
	// default). 'job'  → job detail view (name, schedule, prompt, state,
	// action buttons) anchored via #job-detail injected into #main-pane.
	// Switching back to 'chat' restores the messages/trace/input layout.
	var mainMode = 'chat';
	var activeJobName = '';
	// Last-seen session list, kept so re-renders (after a window switch
	// or new-chat creation) can repaint without a round-trip.
	var sessionsCache = [];
	var toggleToolsBtn = document.getElementById('toggle-tools-btn');
	var inputAreaWrap = document.getElementById('input-area-wrap');
	var attachBtn = document.getElementById('attach-btn');
	var filePicker = document.getElementById('file-picker');
	var attachmentStrip = document.getElementById('attachment-strip');
	var attachErrorEl = document.getElementById('attach-error');

	// Attachment limits — must match decodeChatAttachments in websocket.go.
	var MAX_IMAGE_BYTES = 10 * 1024 * 1024;
	var MAX_DOC_BYTES = 25 * 1024 * 1024;
	var MAX_ATTACHMENT_COUNT = 20;
	var ALLOWED_IMAGE_MIMES = {
		'image/jpeg': true, 'image/png': true, 'image/gif': true,
		'image/webp': true, 'image/bmp': true
	};
	// MIMEs the server will UTF-8 decode directly. Anything matching
	// the text/ prefix is also accepted, alongside this set.
	var ALLOWED_TEXT_MIMES = {
		'application/json': true, 'application/xml': true,
		'application/x-yaml': true, 'application/yaml': true,
		'application/javascript': true, 'application/x-javascript': true,
		'application/x-typescript': true, 'application/typescript': true,
		'application/x-python': true, 'application/x-shellscript': true,
		'application/x-sh': true, 'application/x-ruby': true,
		'application/x-go': true, 'application/x-rust': true,
		'application/x-toml': true, 'application/toml': true,
		'application/sql': true, 'application/x-sql': true,
		'application/x-tex': true
	};
	// MIMEs the server extracts via shell tools (pdftotext, pandoc) or
	// passes natively to capable providers (PDFs on Anthropic / Gemini).
	var ALLOWED_DOC_MIMES = {
		'application/pdf': true,
		'application/vnd.openxmlformats-officedocument.wordprocessingml.document': true
	};
	// Audio MIMEs (PR 4). Only sent when the active agent's provider
	// advertises NativeAudio capability — otherwise rejected client-
	// side with a "switch to Gemini" hint.
	var ALLOWED_AUDIO_MIMES = {
		'audio/mpeg':   true, // .mp3
		'audio/mp4':    true,
		'audio/wav':    true, 'audio/x-wav': true,
		'audio/webm':   true,
		'audio/ogg':    true,
		'audio/flac':   true,
		'audio/aac':    true,
		'audio/x-m4a':  true
	};
	// Browsers leave .md / unusual extensions with empty type. Map a
	// few common text-ish + audio extensions to a canonical MIME so the
	// server allowlist (and the chip kind classifier) accept them.
	var EXT_MIME_FALLBACK = {
		md: 'text/markdown', markdown: 'text/markdown',
		txt: 'text/plain', log: 'text/plain',
		csv: 'text/csv',
		yaml: 'application/x-yaml', yml: 'application/x-yaml',
		toml: 'application/toml',
		json: 'application/json',
		xml: 'application/xml',
		html: 'text/html', htm: 'text/html', css: 'text/css',
		js: 'application/javascript', mjs: 'application/javascript',
		ts: 'application/x-typescript', tsx: 'application/x-typescript',
		py: 'application/x-python',
		sh: 'application/x-sh', bash: 'application/x-sh',
		rb: 'application/x-ruby', go: 'application/x-go',
		rs: 'application/x-rust', sql: 'application/sql',
		tex: 'application/x-tex',
		// Audio extensions
		mp3: 'audio/mpeg', m4a: 'audio/x-m4a',
		wav: 'audio/wav',
		webm: 'audio/webm', ogg: 'audio/ogg', oga: 'audio/ogg',
		flac: 'audio/flac', aac: 'audio/aac'
	};
	function detectMime(file) {
		var t = (file.type || '').toLowerCase();
		if (t) return t;
		var name = (file.name || '').toLowerCase();
		var dot = name.lastIndexOf('.');
		if (dot < 0) return '';
		return EXT_MIME_FALLBACK[name.slice(dot + 1)] || '';
	}
	function isImageMime(m)    { return ALLOWED_IMAGE_MIMES[m] === true; }
	function isPlainTextMime(m){ return m.indexOf('text/') === 0 || ALLOWED_TEXT_MIMES[m] === true; }
	function isDocMime(m)      { return ALLOWED_DOC_MIMES[m] === true; }
	function isAudioMime(m)    { return ALLOWED_AUDIO_MIMES[m] === true; }
	function isAllowedMime(m)  { return isImageMime(m) || isPlainTextMime(m) || isDocMime(m) || isAudioMime(m); }
	function attachmentKind(m) {
		if (isImageMime(m)) return 'image';
		if (isAudioMime(m)) return 'audio';
		if (isDocMime(m)) return 'doc';
		if (isPlainTextMime(m)) return 'text';
		return 'other';
	}

	// Per-agent capabilities cache. Populated from agent.status's
	// per-agent capabilities field, refreshed on agent switch via
	// the agent.capabilities RPC. Used to gate audio uploads at
	// addFiles time so the user gets immediate feedback
	// rather than a server-side rejection after the network round-
	// trip.
	var agentCaps = {}; // agentId → {nativePDF, nativeAudio}
	function activeAgentCaps() {
		var id = agentSelect && agentSelect.value;
		return (id && agentCaps[id]) || { nativePDF: false, nativeAudio: false };
	}
	// Pending attachments for the next chat.send.
	// Each entry: { name, mimeType, kind, sizeBytes, dataB64, objectUrl? }.
	var attachments = [];
	var attachErrorTimer = null;

	// Tool visibility toggle
	var toolsHidden = localStorage.getItem('felix-hide-tools') === 'true';
	function applyToolVisibility() {
		if (toolsHidden) {
			messagesEl.classList.add('hide-tools');
			toggleToolsBtn.classList.remove('active');
		} else {
			messagesEl.classList.remove('hide-tools');
			toggleToolsBtn.classList.add('active');
		}
	}
	applyToolVisibility();
	toggleToolsBtn.addEventListener('click', function() {
		toolsHidden = !toolsHidden;
		localStorage.setItem('felix-hide-tools', toolsHidden);
		applyToolVisibility();
	});

	// Live trace panel
	var toggleTraceBtn = document.getElementById('toggle-trace-btn');
	var tracePanel = document.getElementById('trace-panel');
	var traceList = document.getElementById('trace-list');
	var traceClearBtn = document.getElementById('trace-clear-btn');
	var traceVisible = localStorage.getItem('felix-show-trace') === 'true';
	var traceFirstOfRun = true;
	function applyTraceVisibility() {
		tracePanel.style.display = traceVisible ? 'block' : 'none';
		toggleTraceBtn.classList.toggle('active', traceVisible);
	}
	applyTraceVisibility();
	toggleTraceBtn.addEventListener('click', function() {
		traceVisible = !traceVisible;
		localStorage.setItem('felix-show-trace', traceVisible);
		applyTraceVisibility();
	});
	traceClearBtn.addEventListener('click', function() {
		traceList.innerHTML = '';
	});

	function fmtMs(n) {
		if (n == null) return '';
		if (n < 1000) return n + 'ms';
		return (n / 1000).toFixed(1) + 's';
	}

	// Bootstrap banner — surfaces first-run model pulls from
	// /settings/api/bootstrap so a user landing on /chat sees real
	// progress instead of a chat box that ignores their messages while
	// 10 GB of gemma4 downloads in the background.
	var bootstrapBanner = document.getElementById('bootstrap-banner');
	var bbSummary = document.getElementById('bb-summary');
	var bbModels = document.getElementById('bb-models');
	var inputArea = document.getElementById('input-area');
	var bootstrapPollTimer = null;
	var bootstrapWasActive = false;

	function fmtBytes(n) {
		if (!n || n < 0) return '';
		if (n < 1024) return n + ' B';
		var u = ['KB','MB','GB','TB'];
		var i = -1;
		do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
		return n.toFixed(1) + ' ' + u[i];
	}

	function renderBootstrap(snap) {
		if (!snap || !snap.models) return false;
		var names = Object.keys(snap.models);
		if (names.length === 0) return false;
		names.sort();

		// The banner is shown only when a bootstrap is actually in flight
		// (snap.active === true). The tracker can carry stale per-model
		// state from before EnsureFirstRunModels' early-return path was
		// fixed, so we don't infer "active" from individual model statuses
		// alone — that produced false-positives where the chat input was
		// disabled long after downloads completed.
		if (!snap.active) {
			if (bootstrapWasActive) {
				// Just transitioned active→inactive. Fade after a short
				// victory display so the user sees "ready" briefly.
				setTimeout(function() {
					bootstrapBanner.style.display = 'none';
					inputArea.classList.remove('bootstrapping');
					inputEl.placeholder = 'Type a message...';
				}, 2500);
				bootstrapWasActive = false;
			}
			return false;
		}

		var doneCount = 0;
		bbModels.innerHTML = '';
		names.forEach(function(name) {
			var m = snap.models[name];
			var row = document.createElement('div');
			row.className = 'bb-row';
			if (m.status === 'done') row.classList.add('done');
			if (m.error) row.classList.add('error');

			var nm = document.createElement('div');
			nm.className = 'bb-name';
			nm.textContent = name;

			var bar = document.createElement('div');
			bar.className = 'bb-bar';
			var fill = document.createElement('div');
			fill.className = 'bb-fill';
			fill.style.width = (m.pct || (m.status === 'done' ? 100 : 0)) + '%%';
			bar.appendChild(fill);

			var st = document.createElement('div');
			st.className = 'bb-status';
			if (m.error) {
				st.textContent = 'error';
				st.title = m.error;
			} else if (m.status === 'done') {
				st.textContent = 'ready';
				doneCount++;
			} else if (m.completed && m.total) {
				st.textContent = fmtBytes(m.completed) + '/' + fmtBytes(m.total);
			} else if (m.status) {
				st.textContent = m.status;
			}

			row.appendChild(nm);
			row.appendChild(bar);
			row.appendChild(st);
			bbModels.appendChild(row);
		});

		bbSummary.textContent = doneCount + '/' + names.length + ' ready';
		bootstrapBanner.style.display = 'block';
		inputArea.classList.add('bootstrapping');
		inputEl.placeholder = 'Local AI is downloading… please wait';
		bootstrapWasActive = true;
		return true;
	}

	// Token chip: always visible in the header. Shows the most recent
	// turn's input usage vs the configured context window (so the user
	// always knows how much room they have left), plus output tokens
	// from the last turn. Before any turn fires we show "—/window +—"
	// as a stable baseline. agentWindows maps agentId → context window
	// in tokens; populated from agent.status so switching agents
	// updates the chip even before the first turn on that agent runs.
	var tokenChip = document.getElementById('token-chip');
	var agentWindows = {};
	var lastUsage = null;
	function fmtTokens(n) {
		if (n == null) return '—';
		if (n < 1000) return String(n);
		if (n < 1000000) return (n / 1000).toFixed(n < 10000 ? 1 : 0) + 'K';
		return (n / 1000000).toFixed(2) + 'M';
	}
	function currentWindow() {
		var id = agentSelect && agentSelect.value;
		return (id && agentWindows[id]) || 0;
	}
	function renderTokenChip() {
		if (!tokenChip) return;
		var ctxWindow = currentWindow();
		if (lastUsage) {
			var inTok = (lastUsage.input_tokens || 0) +
				(lastUsage.cache_creation_input_tokens || 0) +
				(lastUsage.cache_read_input_tokens || 0);
			var outTok = lastUsage.output_tokens || 0;
			var pct = ctxWindow > 0 ? (inTok / ctxWindow) * 100 : 0;
			var remaining = ctxWindow > 0 ? Math.max(0, ctxWindow - inTok) : 0;
			tokenChip.textContent = fmtTokens(inTok) +
				(ctxWindow > 0 ? '/' + fmtTokens(ctxWindow) : '') +
				(ctxWindow > 0 ? ' (' + pct.toFixed(0) + '%%)' : '') +
				'  +' + fmtTokens(outTok);
			tokenChip.title = 'Last turn input=' + inTok + ', output=' + outTok +
				(ctxWindow > 0 ? ', remaining=' + remaining + ' / window=' + ctxWindow : '');
			tokenChip.classList.remove('warn', 'danger');
			if (pct >= 80) tokenChip.classList.add('danger');
			else if (pct >= 60) tokenChip.classList.add('warn');
		} else {
			// No turn data yet — show the cleanest baseline that still
			// conveys what's known. With no window AND no turn, the
			// chip just reads "—"; with a window known, show "—/200K"
			// so the user has the context-room number even before
			// their first message lands. The previous "—  +—" form
			// was visual noise nobody could parse without the legend.
			tokenChip.textContent = ctxWindow > 0
				? '— / ' + fmtTokens(ctxWindow)
				: '—';
			tokenChip.title = ctxWindow > 0
				? 'Context window: ' + ctxWindow + ' tokens (no turns yet)'
				: 'Context window unknown';
			tokenChip.classList.remove('warn', 'danger');
		}
	}
	function updateTokenChip(usage /*ctxWindow,model unused; we read from agentWindows*/) {
		if (!usage) return;
		lastUsage = usage;
		renderTokenChip();
	}
	function resetTokenChip() {
		// Called on session switch / new / clear — last-turn usage no
		// longer reflects this session, so drop back to the baseline.
		lastUsage = null;
		renderTokenChip();
	}
	// Initial paint so the chip isn't blank before agent.status arrives.
	renderTokenChip();

	function pollBootstrap() {
		fetch('/settings/api/bootstrap', { cache: 'no-store' })
			.then(function(r) { return r.ok ? r.json() : null; })
			.then(function(snap) {
				var stillActive = renderBootstrap(snap);
				if (bootstrapPollTimer) { clearTimeout(bootstrapPollTimer); bootstrapPollTimer = null; }
				if (stillActive) {
					bootstrapPollTimer = setTimeout(pollBootstrap, 1500);
				}
			})
			.catch(function() { /* endpoint absent / transient — ignore */ });
	}
	pollBootstrap();

	function summarizeAttrs(attrs) {
		if (!attrs) return '';
		var keys = Object.keys(attrs);
		if (keys.length === 0) return '';
		var parts = [];
		for (var i = 0; i < keys.length && parts.length < 3; i++) {
			var k = keys[i];
			var v = attrs[k];
			if (typeof v === 'string' && v.length > 40) v = v.slice(0, 40) + '…';
			parts.push(k + '=' + v);
		}
		return parts.join(' ');
	}

	function addTraceRow(r) {
		// Insert a divider when a new run starts (ws.received is the first
		// mark of every chat.send).
		if (r.phase === 'ws.received') {
			traceFirstOfRun = true;
		}
		var row = document.createElement('div');
		row.className = 'trace-row';
		if (traceFirstOfRun && r.phase === 'ws.received' && traceList.children.length > 0) {
			row.classList.add('run-divider');
		}
		traceFirstOfRun = false;
		if (r.dur_ms != null && r.dur_ms >= 1500) {
			row.classList.add('slow');
		}
		var at = document.createElement('span');
		at.className = 't-at';
		at.textContent = fmtMs(r.at_ms);
		var dur = document.createElement('span');
		dur.className = 't-dur';
		dur.textContent = '+' + fmtMs(r.dur_ms);
		var rest = document.createElement('span');
		rest.className = 't-phase';
		rest.textContent = r.phase;
		var attrText = summarizeAttrs(r.attrs);
		if (attrText) {
			rest.textContent += '  ';
			var a = document.createElement('span');
			a.className = 't-attrs';
			a.textContent = attrText;
			rest.appendChild(a);
		}
		row.appendChild(at);
		row.appendChild(dur);
		row.appendChild(rest);
		traceList.appendChild(row);
		// Keep at most ~500 rows so the panel doesn't grow unbounded.
		while (traceList.children.length > 500) {
			traceList.removeChild(traceList.firstChild);
		}
		tracePanel.scrollTop = tracePanel.scrollHeight;
	}

	// Theme toggle. The icon shows the mode you'd switch *to* — moon
	// while in light mode, sun while in dark mode — so the meaning of
	// a click is "press to enter the displayed mode" rather than
	// "this is the mode you're in", which trips users up.
	function setTheme(mode) {
		if (mode === 'light') {
			document.documentElement.classList.add('light');
			themeBtn.innerHTML = '&#9790;'; // moon ☾
			themeBtn.title = 'Switch to dark mode';
		} else {
			document.documentElement.classList.remove('light');
			themeBtn.innerHTML = '&#9728;'; // sun ☀
			themeBtn.title = 'Switch to light mode';
		}
		localStorage.setItem('felix-theme', mode);
	}

	var saved = localStorage.getItem('felix-theme') || 'dark';
	setTheme(saved);

	themeBtn.addEventListener('click', function() {
		var current = document.documentElement.classList.contains('light') ? 'light' : 'dark';
		setTheme(current === 'light' ? 'dark' : 'light');
	});

	clearBtn.addEventListener('click', function() {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'session.clear',
			params: { agentId: agentSelect.value, sessionKey: activeSessionKey },
			id: 'clear'
		}));
		clearMessagesPane();
		resetTokenChip();
		loadSessions();
	});

	agentSelect.addEventListener('change', function() {
		clearMessagesPane();
		resetTokenChip();
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		// Load sessions for the new agent
		loadSessions();
	});

	function switchToSession(key) {
		if (!key || (key === activeSessionKey && mainMode === 'chat')) return;
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		// If we were viewing a job's detail in the main pane, jump
		// back to the conversation view first.
		if (mainMode === 'job') restoreChatView();
		activeSessionKey = key;
		// Repaint sidebar so the new active row highlights immediately,
		// before the server confirms.
		renderSessionList(sessionsCache);
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'session.switch',
			params: { agentId: agentSelect.value, sessionKey: key },
			id: 'session-switch'
		}));
		clearMessagesPane();
		resetTokenChip();
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'session.history',
			params: { agentId: agentSelect.value, sessionKey: key },
			id: 'history'
		}));
	}

	newChatBtn.addEventListener('click', function() {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		var name = prompt('Conversation name (leave empty for a timestamp):');
		if (name === null) return; // cancelled
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'session.new',
			params: { agentId: agentSelect.value, name: name || '' },
			id: 'session-new'
		}));
	});

	// Sidebar collapse — persisted across reloads. On narrow viewports
	// we use the .open class instead of relying on the persisted state,
	// so an accidental collapse on desktop doesn't leave a phone user
	// without sidebar access.
	(function initSidebar() {
		var collapsed = localStorage.getItem('felix-sidebar-collapsed') === 'true';
		if (collapsed) sidebarEl.classList.add('collapsed');
	})();
	sidebarToggleBtn.addEventListener('click', function() {
		// On narrow viewports, toggle the .open overlay state; on wider
		// viewports, toggle the persistent .collapsed state.
		var narrow = window.matchMedia('(max-width: 700px)').matches;
		if (narrow) {
			sidebarEl.classList.toggle('open');
		} else {
			var nowCollapsed = sidebarEl.classList.toggle('collapsed');
			localStorage.setItem('felix-sidebar-collapsed', nowCollapsed ? 'true' : 'false');
		}
	});

	// Settings cog → /settings in the same tab. Same-tab navigation
	// keeps the user's mental model linear (back button returns them
	// to the chat) and avoids tab proliferation.
	if (settingsBtn) {
		settingsBtn.addEventListener('click', function() {
			window.location.assign('/settings');
		});
	}

	// Sidebar tabs (Threads / Jobs). Switching to Jobs lazily fires
	// jobs.list — there's no point polling when the tab isn't open.
	var activeTab = 'threads';
	function setActiveTab(name) {
		if (activeTab === name) return;
		activeTab = name;
		var onThreads = name === 'threads';
		tabThreadsBtn.classList.toggle('active', onThreads);
		tabThreadsBtn.setAttribute('aria-selected', onThreads ? 'true' : 'false');
		tabJobsBtn.classList.toggle('active', !onThreads);
		tabJobsBtn.setAttribute('aria-selected', onThreads ? 'false' : 'true');
		tabThreadsPane.hidden = !onThreads;
		tabJobsPane.hidden = onThreads;
		if (!onThreads) loadJobs();
	}
	tabThreadsBtn.addEventListener('click', function(){ setActiveTab('threads'); });
	tabJobsBtn.addEventListener('click', function(){ setActiveTab('jobs'); });

	// Jobs tab — basic list rendering. Action cluster + click-to-
	// inspect-in-main-pane comes in the next commit.
	var jobsCache = [];
	function loadJobs() {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'jobs.list',
			params: {},
			id: 'jobs-list'
		}));
	}
	function renderJobsList(jobs) {
		jobsCache = jobs || [];
		// Clear out previous job rows; keep the empty placeholder.
		while (jobsListEl.firstChild &&
				jobsListEl.firstChild.id !== 'jobs-list-empty') {
			jobsListEl.removeChild(jobsListEl.firstChild);
		}
		if (jobsCountEl) {
			if (jobs.length === 0) {
				jobsCountEl.hidden = true;
			} else {
				jobsCountEl.hidden = false;
				jobsCountEl.textContent = String(jobs.length);
			}
		}
		if (!jobs || jobs.length === 0) {
			jobsListEmptyEl.style.display = '';
			return;
		}
		jobsListEmptyEl.style.display = 'none';

		var fragment = document.createDocumentFragment();
		for (var i = 0; i < jobs.length; i++) {
			fragment.appendChild(buildJobRow(jobs[i]));
		}
		jobsListEl.insertBefore(fragment, jobsListEmptyEl);
	}

	// buildJobRow renders one job — title + state + schedule meta,
	// with a state-aware hover action cluster on the right (pause /
	// resume / run-now / view detail). Clicking the row body shows
	// the job detail in the main pane.
	function buildJobRow(j) {
		var row = document.createElement('div');
		row.className = 'job-row';
		row.setAttribute('role', 'button');
		row.setAttribute('tabindex', '0');
		row.dataset.jobName = j.name;
		if (j.paused) row.classList.add('paused');
		if (mainMode === 'job' && activeJobName === j.name) row.classList.add('active');

		var title = document.createElement('span');
		title.className = 'job-title';
		title.textContent = j.name;
		row.appendChild(title);

		var state = document.createElement('span');
		state.className = 'job-state' + (j.paused ? ' paused' : '');
		state.textContent = j.paused ? 'paused' : 'running';
		row.appendChild(state);

		var meta = document.createElement('span');
		meta.className = 'job-meta';
		meta.textContent = j.schedule || '';
		row.appendChild(meta);

		// State-aware action cluster, hover-revealed via CSS.
		var actions = document.createElement('div');
		actions.className = 'job-actions';
		function mkBtn(label, iconId, onClick) {
			var b = document.createElement('button');
			b.type = 'button';
			b.title = label;
			b.setAttribute('aria-label', label);
			b.innerHTML = '<svg class="icon"><use href="#' + iconId + '"/></svg>';
			b.addEventListener('click', function(e) {
				e.stopPropagation();
				onClick();
			});
			return b;
		}
		if (j.paused) {
			actions.appendChild(mkBtn('Resume', 'i-play', function(){ jobsAction('jobs.resume', j.name); }));
			actions.appendChild(mkBtn('Run now', 'i-rerun', function(){ jobsAction('jobs.runNow', j.name); }));
		} else {
			actions.appendChild(mkBtn('Pause', 'i-pause', function(){ jobsAction('jobs.pause', j.name); }));
			actions.appendChild(mkBtn('Run now', 'i-rerun', function(){ jobsAction('jobs.runNow', j.name); }));
		}
		actions.appendChild(mkBtn('View detail', 'i-log', function(){ showJobDetail(j); }));
		row.appendChild(actions);

		row.addEventListener('click', function() { showJobDetail(j); });
		row.addEventListener('keydown', function(e) {
			if (e.key === 'Enter' || e.key === ' ') {
				e.preventDefault();
				showJobDetail(j);
			}
		});
		return row;
	}

	// jobsAction fires a one-shot job RPC and refreshes the list so
	// the row's state reflects the new server-side reality. Errors
	// surface through addError via the WS error path.
	function jobsAction(method, name) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: method,
			params: { name: name },
			id: 'jobs-action-' + name
		}));
		// Give the server a moment to flip the state, then re-list.
		setTimeout(loadJobs, 150);
	}

	// showJobDetail switches the main pane into 'job' mode and paints
	// a detail card for the selected job. Hides the conversation view
	// (messages, trace, input bar) without destroying their state, so
	// switching back is instant.
	function showJobDetail(j) {
		mainMode = 'job';
		activeJobName = j.name;
		// Hide the chat surfaces.
		messagesEl.style.display = 'none';
		var inputAreaWrap = document.getElementById('input-area-wrap');
		if (inputAreaWrap) inputAreaWrap.style.display = 'none';
		var tracePanel = document.getElementById('trace-panel');
		if (tracePanel) tracePanel.dataset.savedDisplay = tracePanel.style.display || '';
		if (tracePanel) tracePanel.style.display = 'none';

		// Find or create the job-detail panel.
		var pane = document.getElementById('job-detail');
		if (!pane) {
			pane = document.createElement('div');
			pane.id = 'job-detail';
			document.getElementById('main-pane').appendChild(pane);
		}
		pane.style.display = '';

		var statusLabel = j.paused ? 'Paused' : 'Running';
		var primaryActionLabel = j.paused ? 'Resume' : 'Pause';
		var primaryActionMethod = j.paused ? 'jobs.resume' : 'jobs.pause';
		var primaryActionIcon = j.paused ? 'i-play' : 'i-pause';

		pane.innerHTML = '';
		var head = document.createElement('div');
		head.className = 'job-detail-head';
		head.innerHTML =
			'<button id="job-detail-back" class="job-detail-back" title="Back to chat" aria-label="Back to chat">&larr; Back to chat</button>' +
			'<h2 class="job-detail-title"></h2>';
		head.querySelector('.job-detail-title').textContent = j.name;
		pane.appendChild(head);

		var statusRow = document.createElement('div');
		statusRow.className = 'job-detail-status';
		statusRow.innerHTML = '<span class="job-state' + (j.paused ? ' paused' : '') + '">' + statusLabel + '</span>';
		pane.appendChild(statusRow);

		function addField(label, value) {
			var f = document.createElement('div');
			f.className = 'job-detail-field';
			var l = document.createElement('div'); l.className = 'job-detail-label'; l.textContent = label;
			var v = document.createElement('div'); v.className = 'job-detail-value'; v.textContent = value || '—';
			f.appendChild(l); f.appendChild(v); pane.appendChild(f);
		}
		addField('Schedule', j.schedule);
		addField('Prompt', j.prompt);

		var actions = document.createElement('div');
		actions.className = 'job-detail-actions';
		function bigBtn(label, iconId, kind, onClick) {
			var b = document.createElement('button');
			b.type = 'button';
			b.className = 'job-detail-btn' + (kind ? ' ' + kind : '');
			b.innerHTML = '<svg class="icon"><use href="#' + iconId + '"/></svg> ' + label;
			b.addEventListener('click', onClick);
			return b;
		}
		actions.appendChild(bigBtn(primaryActionLabel, primaryActionIcon, '', function(){
			jobsAction(primaryActionMethod, j.name);
		}));
		actions.appendChild(bigBtn('Run now', 'i-rerun', '', function(){
			jobsAction('jobs.runNow', j.name);
		}));
		actions.appendChild(bigBtn('Remove', 'i-trash', 'danger', function(){
			if (!confirm('Remove job "' + j.name + '"? It stops running and is deleted.')) return;
			jobsAction('jobs.remove', j.name);
			// After removal, return to chat — the job no longer exists.
			restoreChatView();
		}));
		pane.appendChild(actions);

		var hint = document.createElement('div');
		hint.className = 'job-detail-hint';
		hint.textContent = 'Run history is logged via the gateway slog stream — open the system tray Logs menu to inspect specific runs.';
		pane.appendChild(hint);

		// Wire the back button.
		document.getElementById('job-detail-back').addEventListener('click', restoreChatView);

		// Reflect the highlight on the sidebar row.
		renderJobsList(jobsCache);
	}

	function restoreChatView() {
		if (mainMode !== 'job') return;
		mainMode = 'chat';
		activeJobName = '';
		var pane = document.getElementById('job-detail');
		if (pane) pane.style.display = 'none';
		messagesEl.style.display = '';
		var inputAreaWrap = document.getElementById('input-area-wrap');
		if (inputAreaWrap) inputAreaWrap.style.display = '';
		var tracePanel = document.getElementById('trace-panel');
		if (tracePanel && tracePanel.dataset.savedDisplay !== undefined) {
			tracePanel.style.display = tracePanel.dataset.savedDisplay;
		}
		renderJobsList(jobsCache);
	}

	function loadSessions() {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'session.list',
			params: { agentId: agentSelect.value },
			id: 'sessions'
		}));
	}

	// Group sessions into the same time buckets ChatGPT/Claude.ai use:
	// Today / Yesterday / Last 7 days / Last 30 days / Older. The
	// server gives us lastActivity as a unix timestamp; bucket on the
	// client so the labels stay locale-correct.
	function bucketForSession(s) {
		var now = new Date();
		var startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() / 1000;
		var startOfYesterday = startOfToday - 86400;
		var sevenDaysAgo = startOfToday - 6 * 86400;
		var thirtyDaysAgo = startOfToday - 29 * 86400;
		var t = Number(s.lastActivity || s.createdAt || 0);
		if (t >= startOfToday)     return 'Today';
		if (t >= startOfYesterday) return 'Yesterday';
		if (t >= sevenDaysAgo)     return 'Last 7 days';
		if (t >= thirtyDaysAgo)    return 'Last 30 days';
		return 'Older';
	}
	var BUCKET_ORDER = ['Today', 'Yesterday', 'Last 7 days', 'Last 30 days', 'Older'];

	// renderSessionList paints the sidebar list. Pass [] to render the
	// empty-state placeholder. Active session is determined by the
	// 'active' flag from the server, falling back to activeSessionKey
	// when this client made an unconfirmed switch.
	// Returns the human-readable label for a session — friendly name
	// when one has been set via session.rename, otherwise the bare key.
	function sessionDisplayName(s) {
		var n = (s && s.name) ? String(s.name).trim() : '';
		return n || s.key;
	}

	// buildSessionRow renders one session as a div role=button with a
	// flex title line + ⋮ trigger and an optional preview line below.
	// The ⋮ opens the row context menu (Rename / Pin / Delete).
	function buildSessionRow(s) {
		var row = document.createElement('div');
		row.className = 'session-row';
		row.setAttribute('role', 'button');
		row.setAttribute('tabindex', '0');
		if (s.key === activeSessionKey) row.classList.add('active');
		row.dataset.sessionKey = s.key;
		row.title = sessionDisplayName(s);

		var line = document.createElement('div');
		line.className = 'row-line';

		if (s.pinned) {
			var pinMark = document.createElement('span');
			pinMark.className = 'row-pin-mark';
			pinMark.title = 'Pinned';
			pinMark.innerHTML = '<svg class="icon"><use href="#i-pin"/></svg>';
			line.appendChild(pinMark);
		}

		var titleEl = document.createElement('span');
		titleEl.className = 'row-title';
		titleEl.textContent = sessionDisplayName(s);
		line.appendChild(titleEl);

		var more = document.createElement('button');
		more.type = 'button';
		more.className = 'row-more';
		more.title = 'More';
		more.setAttribute('aria-label', 'More options');
		more.innerHTML = '<svg class="icon"><use href="#i-more"/></svg>';
		more.addEventListener('click', function(e) {
			e.stopPropagation();
			openRowMenu(row, s);
		});
		line.appendChild(more);

		row.appendChild(line);

		var preview = (s.entryCount || 0) + ' message' + (s.entryCount === 1 ? '' : 's');
		var previewEl = document.createElement('span');
		previewEl.className = 'row-preview';
		previewEl.textContent = preview;
		row.appendChild(previewEl);

		row.addEventListener('click', function() {
			if (row.classList.contains('editing')) return;
			switchToSession(s.key);
		});
		row.addEventListener('keydown', function(e) {
			if (row.classList.contains('editing')) return;
			if (e.key === 'Enter' || e.key === ' ') {
				e.preventDefault();
				switchToSession(s.key);
			}
		});
		return row;
	}

	// openRowMenu toggles the popover on the row, closing any other
	// open menus first. Document-level click handler closes when the
	// click lands outside.
	var openMenuRow = null;
	function closeRowMenu() {
		if (!openMenuRow) return;
		var existing = openMenuRow.querySelector('.row-menu');
		if (existing) existing.remove();
		openMenuRow.classList.remove('show-menu');
		openMenuRow = null;
	}
	function openRowMenu(row, s) {
		if (openMenuRow === row) { closeRowMenu(); return; }
		closeRowMenu();
		var menu = document.createElement('div');
		menu.className = 'row-menu';
		menu.addEventListener('click', function(e){ e.stopPropagation(); });

		function mkItem(label, iconId, danger, onClick) {
			var b = document.createElement('button');
			b.type = 'button';
			if (danger) b.className = 'danger';
			b.innerHTML = '<svg class="icon"><use href="#' + iconId + '"/></svg> ' + label;
			b.addEventListener('click', function() {
				closeRowMenu();
				onClick();
			});
			return b;
		}
		menu.appendChild(mkItem('Rename', 'i-pencil', false, function(){ beginRenameRow(row, s); }));
		menu.appendChild(mkItem(s.pinned ? 'Unpin' : 'Pin', 'i-pin', false, function(){ setPinned(s.key, !s.pinned); }));
		// Export lives on each assistant message bubble (hover-revealed
		// download icon) — not in the per-thread row menu — so the user's
		// path-of-least-resistance is to export the specific response
		// they're looking at, not the whole conversation.
		var sep = document.createElement('div');
		sep.className = 'menu-sep';
		menu.appendChild(sep);
		menu.appendChild(mkItem('Delete', 'i-trash', true, function(){ deleteSessionRow(s.key); }));
		row.appendChild(menu);
		row.classList.add('show-menu');
		openMenuRow = row;
	}
	document.addEventListener('click', function(e){
		if (!openMenuRow) return;
		if (openMenuRow.contains(e.target)) return;
		closeRowMenu();
	});

	// Inline rename: swap the title span for an <input>, commit on
	// Enter via session.rename, cancel on Esc or blur.
	function beginRenameRow(row, s) {
		if (row.classList.contains('editing')) return;
		row.classList.add('editing');
		var line = row.querySelector('.row-line');
		var input = document.createElement('input');
		input.type = 'text';
		input.className = 'row-rename-input';
		input.value = sessionDisplayName(s);
		// Insert the input before the ⋮ button so the layout stays.
		var more = row.querySelector('.row-more');
		line.insertBefore(input, more);
		input.focus();
		input.select();
		var cleaned = false;
		function cleanup() {
			if (cleaned) return;
			cleaned = true;
			row.classList.remove('editing');
			input.remove();
		}
		function commit() {
			var v = input.value.trim();
			cleanup();
			renameSession(s.key, v);
		}
		input.addEventListener('keydown', function(e){
			if (e.key === 'Enter') { e.preventDefault(); commit(); }
			else if (e.key === 'Escape') { e.preventDefault(); cleanup(); }
		});
		input.addEventListener('blur', function(){
			// Blur during normal interaction commits, like ChatGPT/
			// Claude — clicking elsewhere accepts the edit rather
			// than discarding it.
			if (!cleaned) commit();
		});
		input.addEventListener('click', function(e){ e.stopPropagation(); });
	}

	function renameSession(key, name) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		// Optimistic update: patch the cache and repaint immediately.
		for (var i = 0; i < sessionsCache.length; i++) {
			if (sessionsCache[i].key === key) {
				sessionsCache[i].name = name;
				break;
			}
		}
		renderSessionList(sessionsCache);
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'session.rename',
			params: { agentId: agentSelect.value, sessionKey: key, name: name },
			id: 'session-rename-' + key
		}));
	}

	function setPinned(key, pinned) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		// Optimistic update — same pattern as rename.
		for (var i = 0; i < sessionsCache.length; i++) {
			if (sessionsCache[i].key === key) {
				sessionsCache[i].pinned = pinned;
				break;
			}
		}
		renderSessionList(sessionsCache);
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'session.setPinned',
			params: { agentId: agentSelect.value, sessionKey: key, pinned: pinned },
			id: 'session-pin-' + key
		}));
	}

	// openExportDialog presents a small modal with format buttons and
	// an "include tool calls" checkbox. Click a format → download
	// starts via a hidden <a> with the right href; PDF opens the
	// HTML export in a new window and triggers window.print() so the
	// user gets the OS's native "Save as PDF" dialog without the
	// gateway needing LaTeX or wkhtmltopdf installed.
	function openExportDialog(s, messageIndex) {
		// messageIndex is optional — when provided (>= 0) the dialog
		// scopes the download to a single assistant message + the user
		// prompt that drove it. Otherwise it falls back to a full-
		// conversation export.
		var perMessage = (typeof messageIndex === 'number' && messageIndex >= 0);

		// Close any prior dialog first.
		var existing = document.getElementById('export-dialog');
		if (existing) existing.remove();

		var overlay = document.createElement('div');
		overlay.id = 'export-dialog';
		overlay.className = 'export-overlay';
		var heading = perMessage ? 'Export this response' : 'Export conversation';
		var sub = perMessage
			? 'Message ' + (messageIndex + 1) + ' from "' + escHtml(sessionDisplayName(s)) + '"'
			: escHtml(sessionDisplayName(s));
		overlay.innerHTML = '<div class="export-card" role="dialog" aria-modal="true" aria-labelledby="export-title">' +
			'<h3 id="export-title">' + heading + '</h3>' +
			'<div class="export-sub">' + sub + '</div>' +
			'<div class="export-formats">' +
				'<button class="export-fmt" data-fmt="md"   title="Markdown (.md)">Markdown</button>' +
				'<button class="export-fmt" data-fmt="txt"  title="Plain text (.txt)">Text</button>' +
				'<button class="export-fmt" data-fmt="html" title="HTML page (.html)">HTML</button>' +
				'<button class="export-fmt" data-fmt="docx" title="Word document (.docx) — needs pandoc on PATH">Word</button>' +
				'<button class="export-fmt" data-fmt="pdf"  title="Opens the HTML export in a new window and triggers Save as PDF">PDF</button>' +
				'<button class="export-fmt" data-fmt="json" title="Raw session JSONL (lossless archive)">JSON</button>' +
			'</div>' +
			'<label class="export-toggle">' +
				'<input type="checkbox" id="export-tools"> Include tool calls and results' +
			'</label>' +
			'<div class="export-actions">' +
				'<button id="export-cancel" class="export-cancel-btn">Cancel</button>' +
			'</div>' +
		'</div>';
		document.body.appendChild(overlay);

		function close() { overlay.remove(); }
		overlay.addEventListener('click', function(e) {
			// Click on the overlay backdrop (outside the card) closes.
			if (e.target === overlay) close();
		});
		document.getElementById('export-cancel').addEventListener('click', close);
		document.addEventListener('keydown', function escClose(e) {
			if (e.key === 'Escape') {
				document.removeEventListener('keydown', escClose);
				close();
			}
		});

		var includeToolsEl = document.getElementById('export-tools');
		Array.from(overlay.querySelectorAll('.export-fmt')).forEach(function(btn) {
			btn.addEventListener('click', function() {
				var fmt = btn.dataset.fmt;
				var includeTools = includeToolsEl.checked;
				doExport(s.key, fmt, includeTools, perMessage ? messageIndex : -1);
				close();
			});
		});
	}

	// openExportDialogForMessage is the entry point used by the
	// per-bubble export icon. It resolves the active session (so the
	// dialog can show the friendly name) and forwards messageIndex.
	function openExportDialogForMessage(messageIndex) {
		if (!activeSessionKey) return;
		var s = null;
		for (var i = 0; i < sessionsCache.length; i++) {
			if (sessionsCache[i].key === activeSessionKey) { s = sessionsCache[i]; break; }
		}
		if (!s) s = { key: activeSessionKey, name: '', preview: '' };
		openExportDialog(s, messageIndex);
	}

	// doExport drives one export. For 'pdf' it opens the HTML export
	// in a new window and triggers window.print() once loaded — the
	// browser's print dialog has a built-in "Save as PDF" option, so
	// no server-side LaTeX/wkhtmltopdf is required. For the rest, a
	// hidden <a download> link kicks off the file download.
	function doExport(key, fmt, includeTools, messageIndex) {
		var base = '/api/session/export?agentId=' +
			encodeURIComponent(agentSelect.value) +
			'&sessionKey=' + encodeURIComponent(key) +
			'&includeTools=' + (includeTools ? 'true' : 'false');
		if (typeof messageIndex === 'number' && messageIndex >= 0) {
			base += '&messageIndex=' + messageIndex;
		}
		if (fmt === 'pdf') {
			var url = base + '&format=html';
			var w = window.open(url, '_blank');
			if (!w) {
				// Pop-up blocked — fall back to in-tab navigation
				// with a hint that the user should print from there.
				window.location.href = url;
				return;
			}
			// Wait for the new window to load, then trigger print.
			// Some browsers block the call when the window is still
			// loading; the onload handler fires after the HTML is
			// painted.
			w.addEventListener('load', function() {
				try { w.focus(); w.print(); } catch (_) {}
			});
			return;
		}
		var a = document.createElement('a');
		a.href = base + '&format=' + encodeURIComponent(fmt);
		a.download = ''; // server's Content-Disposition picks the name
		document.body.appendChild(a);
		a.click();
		document.body.removeChild(a);
	}

	function deleteSessionRow(key) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		var label = key;
		for (var i = 0; i < sessionsCache.length; i++) {
			if (sessionsCache[i].key === key) { label = sessionDisplayName(sessionsCache[i]); break; }
		}
		if (!confirm('Delete "' + label + '"? This removes the conversation history permanently.')) return;
		// session.clear with the existing handler is destructive in the
		// sense that it wipes history — but the sessions list still
		// shows the (now empty) session. For full removal we'd need a
		// dedicated session.delete RPC; until then, clear the history
		// and reload the list so the session falls back to a fresh state.
		// TODO: add session.delete on the server when this UI lands.
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'session.clear',
			params: { agentId: agentSelect.value, sessionKey: key },
			id: 'clear'
		}));
		clearMessagesPane();
		resetTokenChip();
		setTimeout(loadSessions, 50);
	}

	function renderSessionList(sessions) {
		sessionsCache = sessions || [];
		while (sessionListEl.firstChild &&
				sessionListEl.firstChild.id !== 'session-list-empty') {
			sessionListEl.removeChild(sessionListEl.firstChild);
		}
		if (!sessions || sessions.length === 0) {
			sessionListEmptyEl.style.display = '';
			return;
		}
		sessionListEmptyEl.style.display = 'none';

		// Pinned section first, then time buckets in recency order.
		var pinned = [];
		var unpinned = [];
		for (var i = 0; i < sessions.length; i++) {
			(sessions[i].pinned ? pinned : unpinned).push(sessions[i]);
		}
		pinned.sort(function(a,b){ return Number(b.lastActivity||0) - Number(a.lastActivity||0); });
		unpinned.sort(function(a,b){ return Number(b.lastActivity||0) - Number(a.lastActivity||0); });

		var fragment = document.createDocumentFragment();

		if (pinned.length > 0) {
			var hdr = document.createElement('div');
			hdr.className = 'session-group-label';
			hdr.textContent = 'Pinned';
			fragment.appendChild(hdr);
			for (var p = 0; p < pinned.length; p++) {
				fragment.appendChild(buildSessionRow(pinned[p]));
			}
		}

		var grouped = {};
		for (var u = 0; u < unpinned.length; u++) {
			var b = bucketForSession(unpinned[u]);
			(grouped[b] = grouped[b] || []).push(unpinned[u]);
		}
		for (var g = 0; g < BUCKET_ORDER.length; g++) {
			var label = BUCKET_ORDER[g];
			var group = grouped[label];
			if (!group || group.length === 0) continue;
			var hdr2 = document.createElement('div');
			hdr2.className = 'session-group-label';
			hdr2.textContent = label;
			fragment.appendChild(hdr2);
			for (var j = 0; j < group.length; j++) {
				fragment.appendChild(buildSessionRow(group[j]));
			}
		}
		sessionListEl.insertBefore(fragment, sessionListEmptyEl);
	}

	function clearMessagesPane() {
		messagesEl.innerHTML = '';
		messagesEl.classList.remove('has-messages');
		// Re-insert the empty-state element so it re-shows when a
		// session is freshly cleared.
		var empty = document.createElement('div');
		empty.id = 'messages-empty';
		empty.innerHTML = '<div class="empty-title">Start a conversation</div>' +
			'<div class="empty-hint">Type a message below, drop a file onto the window, or paste an image from the clipboard. Conversations save automatically and appear in the sidebar.</div>';
		messagesEl.appendChild(empty);
		currentAssistant = null;
		toolEls = {};
		// Reset the per-bubble export counter — the next assistant
		// message rendered into a fresh session is "message 0".
		assistantMsgCounter = 0;
	}

	var ws = null;
	var msgId = 0;
	var currentAssistant = null;
	var sending = false;
	var reconnectTimer = null;

	// Inline markdown: code, bold, italic, links
	function inlineMd(s) {
		// Extract inline code spans into placeholders (before HTML escaping)
		var codeSpans = [];
		s = s.replace(/` + "`" + `([^` + "`" + `]+)` + "`" + `/g, function(_, code) {
			var idx = codeSpans.length;
			codeSpans.push('<code>' + escHtml(code) + '</code>');
			return '\x00CS' + idx + '\x00';
		});
		// Escape HTML in all remaining text to prevent XSS
		s = escHtml(s);
		// Apply formatting on the now-safe text
		s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
		s = s.replace(/\*(.+?)\*/g, '<em>$1</em>');
		// Validate link URLs — block javascript: and data: schemes
		s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, function(m, text, url) {
			var lower = url.trim().toLowerCase();
			if (lower.indexOf('javascript:') === 0 || lower.indexOf('data:') === 0 || lower.indexOf('vbscript:') === 0) {
				return text;
			}
			return '<a href="' + url + '" target="_blank" rel="noopener">' + text + '</a>';
		});
		// Restore code spans
		for (var i = 0; i < codeSpans.length; i++) {
			s = s.replace('\x00CS' + i + '\x00', codeSpans[i]);
		}
		return s;
	}

	// Block-level markdown renderer
	function renderMd(text) {
		// 1. Extract code blocks into placeholders
		var codeBlocks = [];
		var s = text.replace(/` + "```" + `(\w*)\n([\s\S]*?)` + "```" + `/g, function(_, lang, code) {
			var idx = codeBlocks.length;
			codeBlocks.push('<pre><code>' + escHtml(code.trimEnd()) + '</code></pre>');
			return '__CB' + idx + '__';
		});

		// 2. Process lines
		var lines = s.split('\n');
		var html = '';
		var inUl = false, inOl = false, inP = false;

		function closeAll() {
			if (inP) { html += '</p>'; inP = false; }
			if (inUl) { html += '</ul>'; inUl = false; }
			if (inOl) { html += '</ol>'; inOl = false; }
		}

		for (var i = 0; i < lines.length; i++) {
			var t = lines[i].trim();

			// Code block placeholder
			if (/^__CB\d+__$/.test(t)) {
				closeAll();
				html += t;
				continue;
			}

			// Empty line — close paragraphs but keep lists open
			// (loose list items separated by blank lines stay in the same list)
			if (t === '') {
				if (inP) { html += '</p>'; inP = false; }
				continue;
			}

			// Horizontal rule (before list check so --- isn't a list item)
			if (/^[-*_]{3,}$/.test(t)) {
				closeAll();
				html += '<hr>';
				continue;
			}

			// Heading
			var hm = t.match(/^(#{1,6})\s+(.*)$/);
			if (hm) {
				closeAll();
				var lvl = hm[1].length;
				html += '<h' + lvl + '>' + inlineMd(hm[2]) + '</h' + lvl + '>';
				continue;
			}

			// Unordered list item
			var um = t.match(/^[-*]\s+(.*)$/);
			if (um) {
				if (inP) { html += '</p>'; inP = false; }
				if (inOl) { html += '</ol>'; inOl = false; }
				if (!inUl) { html += '<ul>'; inUl = true; }
				html += '<li>' + inlineMd(um[1]) + '</li>';
				continue;
			}

			// Ordered list item
			var om = t.match(/^\d+[.)]\s+(.*)$/);
			if (om) {
				if (inP) { html += '</p>'; inP = false; }
				if (inUl) { html += '</ul>'; inUl = false; }
				if (!inOl) { html += '<ol>'; inOl = true; }
				html += '<li>' + inlineMd(om[1]) + '</li>';
				continue;
			}

			// Table: line starts with | and next line is a separator row
			if (t.charAt(0) === '|' && i + 1 < lines.length) {
				var sepLine = lines[i + 1].trim();
				if (/^\|[\s\-:]+(\|[\s\-:]+)+\|?\s*$/.test(sepLine)) {
					closeAll();
					// Parse alignment from separator
					var sepCells = sepLine.replace(/^\||\|$/g, '').split('|');
					var aligns = [];
					for (var a = 0; a < sepCells.length; a++) {
						var sc = sepCells[a].trim();
						if (sc.charAt(0) === ':' && sc.charAt(sc.length - 1) === ':') aligns.push('center');
						else if (sc.charAt(sc.length - 1) === ':') aligns.push('right');
						else aligns.push('left');
					}
					// Parse header row
					var hdrs = t.replace(/^\||\|$/g, '').split('|');
					var tbl = '<table><thead><tr>';
					for (var h = 0; h < hdrs.length; h++) {
						var al = aligns[h] || 'left';
						tbl += '<th style="text-align:' + al + '">' + inlineMd(hdrs[h].trim()) + '</th>';
					}
					tbl += '</tr></thead><tbody>';
					// Skip separator line
					i += 2;
					// Parse body rows
					while (i < lines.length && lines[i].trim().charAt(0) === '|') {
						var cells = lines[i].trim().replace(/^\||\|$/g, '').split('|');
						tbl += '<tr>';
						for (var c = 0; c < cells.length; c++) {
							var cal = aligns[c] || 'left';
							tbl += '<td style="text-align:' + cal + '">' + inlineMd(cells[c].trim()) + '</td>';
						}
						tbl += '</tr>';
						i++;
					}
					tbl += '</tbody></table>';
					html += tbl;
					i--; // compensate for loop increment
					continue;
				}
			}

			// Regular text — close any open list first
			if (inUl) { html += '</ul>'; inUl = false; }
			if (inOl) { html += '</ol>'; inOl = false; }
			if (inP) {
				html += '<br>' + inlineMd(t);
			} else {
				html += '<p>' + inlineMd(t);
				inP = true;
			}
		}

		if (inP) html += '</p>';
		if (inUl) html += '</ul>';
		if (inOl) html += '</ol>';

		// 3. Restore code blocks
		for (var i = 0; i < codeBlocks.length; i++) {
			html = html.replace('__CB' + i + '__', codeBlocks[i]);
		}

		return html;
	}

	function escHtml(s) {
		return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
	}

	function scrollToBottom() {
		messagesEl.scrollTop = messagesEl.scrollHeight;
	}

	function connect() {
		ws = new WebSocket(wsBase + '/ws');

		ws.onopen = function() {
			connStatus.textContent = 'live';
			connStatus.className = 'connected';
			sendBtn.disabled = false;
			if (reconnectTimer) {
				clearTimeout(reconnectTimer);
				reconnectTimer = null;
			}
			// Fetch available agents first, then load history
			ws.send(JSON.stringify({
				jsonrpc: '2.0',
				method: 'agent.status',
				params: {},
				id: 'agents'
			}));
		};

		ws.onclose = function() {
			connStatus.textContent = 'reconnecting';
			connStatus.className = 'connecting';
			sendBtn.disabled = true;
			sending = false;
			reconnectTimer = setTimeout(connect, 3000);
		};

		ws.onerror = function() {
			connStatus.textContent = 'error';
			connStatus.className = 'error';
		};

		ws.onmessage = function(e) {
			try {
				var resp = JSON.parse(e.data);
				if (resp.error) {
					addError(typeof resp.error === 'string' ? resp.error : resp.error.message || JSON.stringify(resp.error));
					sending = false;
					updateSendBtn();
					return;
				}
				if (!resp.result) return;

				// Handle agent.status response
				if (resp.id === 'agents') {
					var agents = resp.result.agents || [];
					agentSelect.innerHTML = '';
					agentWindows = {};
					agentCaps = {};
					for (var i = 0; i < agents.length; i++) {
						var opt = document.createElement('option');
						opt.value = agents[i].id;
						opt.textContent = agents[i].name || agents[i].id;
						agentSelect.appendChild(opt);
						if (agents[i].context_window) {
							agentWindows[agents[i].id] = agents[i].context_window;
						}
						// PR 4: per-agent multi-modal capabilities. Used
						// by addFiles to gate audio uploads on the
						// active agent's provider before the WS round-
						// trip surfaces a server-side rejection.
						if (agents[i].capabilities) {
							agentCaps[agents[i].id] = {
								nativePDF:   agents[i].capabilities.nativePDF === true,
								nativeAudio: agents[i].capabilities.nativeAudio === true
							};
						}
					}
					renderTokenChip();
					if (agents.length === 0) {
						var opt = document.createElement('option');
						opt.value = 'default';
						opt.textContent = 'default';
						agentSelect.appendChild(opt);
					}
					// Load sessions for the selected agent
					loadSessions();
					return;
				}

				// Handle session.list response — paint the sidebar list
				// and (re)load history for whichever row is active.
				if (resp.id === 'sessions') {
					var sessions = resp.result.sessions || [];
					// Pick the active key: server-side flag wins, else
					// fall back to whatever this client last switched to,
					// else first session, else the default key.
					var nextActive = '';
					for (var i = 0; i < sessions.length; i++) {
						if (sessions[i].active) { nextActive = sessions[i].key; break; }
					}
					if (!nextActive && activeSessionKey) {
						for (var i = 0; i < sessions.length; i++) {
							if (sessions[i].key === activeSessionKey) { nextActive = activeSessionKey; break; }
						}
					}
					if (!nextActive && sessions.length > 0) nextActive = sessions[0].key;
					if (!nextActive) nextActive = 'ws_default';
					activeSessionKey = nextActive;
					renderSessionList(sessions);
					// Load history for the active session
					clearMessagesPane();
					ws.send(JSON.stringify({
						jsonrpc: '2.0',
						method: 'session.history',
						params: { agentId: agentSelect.value, sessionKey: activeSessionKey },
						id: 'history'
					}));
					return;
				}

				// Handle session.new response
				if (resp.id === 'session-new') {
					if (resp.result && resp.result.sessionKey) {
						loadSessions();
					}
					return;
				}

				// Handle session.switch response
				if (resp.id === 'session-switch') {
					return;
				}

				// jobs.list response — paint the Jobs tab. Only fired
				// when the user actually opens the Jobs tab (lazy).
				if (resp.id === 'jobs-list') {
					var jobs = (resp.result && resp.result.jobs) || [];
					renderJobsList(jobs);
					return;
				}

				// session.rename / setPinned acks: the optimistic
				// repaint in the UI already handled the visual update;
				// the server confirmation is logged but otherwise
				// silent. If it errored, addError surfaces it.
				if (typeof resp.id === 'string' && (
					resp.id.indexOf('session-rename-') === 0 ||
					resp.id.indexOf('session-pin-') === 0)) {
					return;
				}

				// Handle history response
				if (resp.id === 'history') {
					var entries = resp.result.entries || [];
					for (var i = 0; i < entries.length; i++) {
						var entry = entries[i];
						if (entry.type === 'message' && entry.role === 'user') {
							addUserMsg(entry.text);
						} else if (entry.type === 'message' && entry.role === 'assistant') {
							var bubble = addAssistantMsg();
							bubble.raw = entry.text;
							bubble.content.innerHTML = renderMd(entry.text);
						} else if (entry.type === 'tool_call') {
							addToolCall(entry.tool, entry.id, entry.input);
						} else if (entry.type === 'tool_result') {
							updateToolResult(null, entry.tool_call_id, null, entry.output, entry.error, entry.images);
						}
					}
					scrollToBottom();
					return;
				}

				var r = resp.result;

				switch (r.type) {
				case 'text_delta':
					if (!currentAssistant) {
						currentAssistant = addAssistantMsg('');
					}
					appendToAssistant(r.text);
					break;
				case 'tool_call_start':
					if (currentAssistant) {
						finalizeAssistant();
						currentAssistant = null;
					}
					addToolCall(r.tool, r.id, r.input);
					break;
				case 'tool_result':
					updateToolResult(r.tool, r.id, r.input, r.output, r.error, r.images, r.auth_required);
					break;
				case 'done':
					if (currentAssistant) {
						finalizeAssistant();
					}
					currentAssistant = null;
					sending = false;
					updateSendBtn();
					if (r.context_window && agentSelect && agentSelect.value) {
					// Per-turn context_window is the freshest server value
					// (config hot-reload could have changed it since
					// agent.status fired). Cache it back so subsequent
					// renders stay consistent.
					agentWindows[agentSelect.value] = r.context_window;
				}
				updateTokenChip(r.usage);
					break;
				case 'aborted':
					if (currentAssistant) {
						finalizeAssistant();
					}
					currentAssistant = null;
					sending = false;
					updateSendBtn();
					break;
				case 'error':
					addError(r.message);
					currentAssistant = null;
					sending = false;
					updateSendBtn();
					break;
				case 'trace':
					addTraceRow(r);
					break;
				}
			} catch(err) {
				console.error('parse error:', err);
			}
		};
	}

	function addUserMsg(text, atts) {
		messagesEl.classList.add('has-messages');
		var div = document.createElement('div');
		div.className = 'msg user';
		// dir=auto + unicode-bidi:plaintext on the bubble lets each
		// paragraph resolve its direction from its first strong-
		// directional character — Arabic / Hebrew lines flow RTL,
		// LTR scripts stay LTR, mixed paragraphs work without manual
		// toggling.
		div.setAttribute('dir', 'auto');
		if (text) {
			var p = document.createElement('div');
			p.textContent = text;
			div.appendChild(p);
		}
		if (atts && atts.length > 0) {
			var strip = document.createElement('div');
			strip.className = 'user-attachments';
			for (var i = 0; i < atts.length; i++) {
				var a = atts[i];
				if (a.kind === 'image') {
					var img = document.createElement('img');
					// Prefer the live object URL (cheap, no re-encoding); fall
					// back to a data: URL so historical replays still render
					// even after the originating object URL is revoked.
					img.src = a.objectUrl ||
						('data:' + a.mimeType + ';base64,' + a.dataB64);
					img.alt = a.name || 'attachment';
					// Same broken-image fallback as the chip strip — if
					// the bytes can't be decoded, render a labelled pill
					// instead of leaving the browser's broken icon.
					img.addEventListener('error', function() {
						var pill = document.createElement('span');
						pill.className = 'user-attachment-pill';
						pill.dataset.kind = 'image';
						pill.textContent = a.name || 'image';
						pill.title = a.mimeType + ' · could not render preview';
						img.replaceWith(pill);
					});
					strip.appendChild(img);
				} else {
					// Doc/text attachment — render as a non-removable mini chip
					// so the user sees what was attached without re-encoding
					// arbitrary bytes for inline display.
					var pill = document.createElement('span');
					pill.className = 'user-attachment-pill';
					pill.dataset.kind = a.kind || 'other';
					pill.textContent = a.name || 'attachment';
					pill.title = a.mimeType + ' · ' + formatBytes(a.sizeBytes);
					strip.appendChild(pill);
				}
			}
			div.appendChild(strip);
		}
		messagesEl.appendChild(div);
		scrollToBottom();
	}

	// Counter for the Nth assistant message in the currently-rendered
	// session. Bumped by addAssistantMsg, reset by clearMessagesPane.
	// The value is baked into each assistant bubble as data-msg-idx so
	// the per-bubble Export button can pass messageIndex=N to the
	// /api/session/export endpoint.
	var assistantMsgCounter = 0;

	function addAssistantMsg() {
		messagesEl.classList.add('has-messages');
		var div = document.createElement('div');
		div.className = 'msg assistant';
		var idx = assistantMsgCounter++;
		div.setAttribute('data-msg-idx', String(idx));
		var content = document.createElement('div');
		content.className = 'content';
		// Same dir=auto / unicode-bidi:plaintext treatment as user
		// bubbles — assistant replies in mixed-script render with the
		// correct per-paragraph direction.
		content.setAttribute('dir', 'auto');
		div.appendChild(content);
		// Per-message Export button — hover-revealed download icon on
		// the top-right of the bubble. Click opens the export dialog
		// scoped to this message (server resolves Nth assistant turn
		// + the user prompt that drove it).
		var exportBtn = document.createElement('button');
		exportBtn.className = 'msg-export-btn';
		exportBtn.type = 'button';
		exportBtn.setAttribute('aria-label', 'Export this response');
		exportBtn.setAttribute('title', 'Export this response (Markdown / Text / HTML / Word / PDF / JSON)');
		exportBtn.innerHTML = '<svg width="14" height="14" aria-hidden="true"><use href="#i-export"></use></svg>';
		exportBtn.addEventListener('click', function(e) {
			e.stopPropagation();
			openExportDialogForMessage(idx);
		});
		div.appendChild(exportBtn);
		messagesEl.appendChild(div);
		scrollToBottom();
		return { el: div, content: content, raw: '' };
	}

	function appendToAssistant(text) {
		if (!currentAssistant) return;
		currentAssistant.raw += text;
		currentAssistant.content.innerHTML = renderMd(currentAssistant.raw);
		scrollToBottom();
	}

	function finalizeAssistant() {
		if (!currentAssistant) return;
		currentAssistant.content.innerHTML = renderMd(currentAssistant.raw);
		scrollToBottom();
	}

	var toolEls = {};

	function toolSummary(toolName, input) {
		if (!input) return escHtml(toolName);
		try {
			var p = (typeof input === 'string') ? JSON.parse(input) : input;
			switch (toolName) {
			case 'bash':
				if (p.command) return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(p.command) + '</span>';
				break;
			case 'read_file':
				if (p.path) return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(p.path) + '</span>';
				break;
			case 'write_file':
				if (p.path) return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(p.path) + '</span>';
				break;
			case 'edit_file':
				if (p.path) return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(p.path) + '</span>';
				break;
			case 'web_fetch':
				if (p.url) return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(p.url) + '</span>';
				break;
			case 'web_search':
				if (p.query) return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(p.query) + '</span>';
				break;
			case 'browser':
				if (p.action) {
					var detail = p.action;
					if (p.url) detail += ' ' + p.url;
					else if (p.selector) detail += ' ' + p.selector;
					return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(detail) + '</span>';
				}
				break;
			case 'send_message':
				if (p.channel) return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(p.channel + ' → ' + (p.chat_id || '')) + '</span>';
				break;
			case 'cron':
				if (p.action) return escHtml(toolName) + ': <span class="tool-detail">' + escHtml(p.action + (p.name ? ' ' + p.name : '')) + '</span>';
				break;
			}
		} catch(e) {}
		return escHtml(toolName);
	}

	function addToolCall(toolName, toolId, input) {
		messagesEl.classList.add('has-messages');
		var div = document.createElement('div');
		div.className = 'tool-call';
		var id = toolId || toolName;
		div.dataset.toolId = id;

		var header = document.createElement('div');
		header.className = 'tool-call-header';
		header.innerHTML = '<span class="arrow">&#9654;</span> ' + toolSummary(toolName, input);
		header.onclick = function() {
			var arrow = header.querySelector('.arrow');
			var output = div.querySelector('.tool-call-output');
			if (output) {
				output.classList.toggle('show');
				arrow.classList.toggle('open');
			}
		};

		var output = document.createElement('div');
		output.className = 'tool-call-output';

		div.appendChild(header);
		div.appendChild(output);
		messagesEl.appendChild(div);
		toolEls[id] = div;
		scrollToBottom();
	}

	function updateToolResult(toolName, toolId, input, outputText, errorText, images, authRequired) {
		var el = toolEls[toolId] || toolEls[toolName];
		if (!el) return;

		// Update header with input if we now have it
		if (input) {
			var header = el.querySelector('.tool-call-header');
			if (header) {
				var arrow = header.querySelector('.arrow');
				var isOpen = arrow && arrow.classList.contains('open');
				header.innerHTML = '<span class="arrow' + (isOpen ? ' open' : '') + '">&#9654;</span> ' + toolSummary(toolName, input);
				header.onclick = function() {
					var a = header.querySelector('.arrow');
					var o = el.querySelector('.tool-call-output');
					if (o) { o.classList.toggle('show'); a.classList.toggle('open'); }
				};
			}
		}

		var output = el.querySelector('.tool-call-output');
		if (!output) return;
		if (errorText) {
			output.textContent = errorText;
			output.classList.add('error');
		} else if (outputText) {
			var display = outputText.length > 2000 ? outputText.substring(0, 2000) + '\n...(truncated)' : outputText;
			output.textContent = display;
		} else {
			output.textContent = '(no output)';
		}

		// Render images (e.g. browser screenshots)
		if (images && images.length > 0) {
			output.classList.add('has-image');
			for (var i = 0; i < images.length; i++) {
				var img = document.createElement('img');
				img.src = 'data:' + images[i].mimeType + ';base64,' + images[i].data;
				img.title = 'Click to open full size';
				img.onclick = (function(src) {
					return function() { window.open(src, '_blank'); };
				})(img.src);
				output.appendChild(img);
			}
			// Auto-expand to show the image
			output.classList.add('show');
			var arrow = el.querySelector('.arrow');
			if (arrow) arrow.classList.add('open');
		}

		// Inline "Re-authenticate" button when the MCP adapter flagged
		// this result as auth-failure. POSTs to /api/mcp/reauth/{id};
		// the user's browser opens the IdP, the gateway loopback
		// listener catches the callback, the manager reconnects, and
		// the button is replaced with a status line. No restart needed.
		if (authRequired) {
			var existing = el.querySelector('.mcp-reauth');
			if (existing) existing.remove();
			var box = document.createElement('div');
			box.className = 'mcp-reauth';
			var msg = document.createElement('span');
			msg.textContent = 'MCP server "' + authRequired + '" needs re-authentication.';
			var btn = document.createElement('button');
			btn.type = 'button';
			btn.textContent = 'Re-authenticate';
			btn.onclick = function() {
				btn.disabled = true;
				btn.textContent = 'Opening browser…';
				fetch('/api/mcp/reauth/' + encodeURIComponent(authRequired), { method: 'POST' })
					.then(function(r) { return r.json().catch(function() { return {ok: false, error: 'invalid response'}; }); })
					.then(function(j) {
						if (j && j.ok) {
							msg.textContent = 'Re-authenticated. Retry your last message.';
							if (j.warning) {
								msg.textContent += ' Note: ' + j.warning;
							}
							btn.remove();
							box.classList.add('ok');
						} else {
							btn.disabled = false;
							btn.textContent = 'Re-authenticate';
							msg.textContent = 'Re-auth failed: ' + (j && j.error ? j.error : 'unknown error');
							box.classList.add('error');
						}
					})
					.catch(function(e) {
						btn.disabled = false;
						btn.textContent = 'Re-authenticate';
						msg.textContent = 'Re-auth request failed: ' + e;
						box.classList.add('error');
					});
			};
			box.appendChild(msg);
			box.appendChild(btn);
			// Append after the output area, inside the tool element so
			// it appears right under the failed call.
			el.appendChild(box);
			// Auto-expand the output so the user sees the button.
			output.classList.add('show');
			var arrow2 = el.querySelector('.arrow');
			if (arrow2) arrow2.classList.add('open');
		}
	}

	// friendlyError maps a raw error string from the agent runtime / LLM
	// provider into a non-technical title + suggestion + (optional) link
	// to a settings tab. Falls back to the raw error so we never lose info.
	function friendlyError(raw) {
		var s = String(raw || '').toLowerCase();
		// Anthropic / OpenAI rate limits.
		if (s.indexOf('rate_limit') >= 0 || s.indexOf('429') >= 0) {
			return {
				title: 'Hit the provider rate limit',
				suggest: 'Wait a minute and try again. To switch models automatically next time, set a fallback model in Settings → Agents.',
				settings: 'agents'
			};
		}
		// Anthropic 529 / OpenAI 5xx — provider overloaded.
		if (s.indexOf('overloaded') >= 0 || s.indexOf('529') >= 0 || /\b5\d\d\b/.test(s)) {
			return {
				title: 'The model provider is overloaded',
				suggest: 'Try again in a minute. If this is persistent, switch to a different provider in Settings → Providers.',
				settings: 'providers'
			};
		}
		// Context overflow.
		if (s.indexOf('context length') >= 0 || s.indexOf('context_length') >= 0 || s.indexOf('too long') >= 0) {
			return {
				title: 'Conversation is too long for the model',
				suggest: 'Start a new session, or enable / lower the compaction threshold in Settings → Intelligence → Compaction.',
				settings: 'intelligence'
			};
		}
		// Missing API key / auth.
		if (s.indexOf('api key') >= 0 || s.indexOf('api_key') >= 0 || s.indexOf('unauthorized') >= 0 || s.indexOf('401') >= 0 || s.indexOf('403') >= 0) {
			return {
				title: 'Missing or invalid API key',
				suggest: 'Add your API key in Settings → Providers, then save and try again.',
				settings: 'providers'
			};
		}
		// LLM provider not configured at all.
		if (s.indexOf('llm provider not configured') >= 0 || s.indexOf('no api key') >= 0) {
			return {
				title: 'No LLM provider is configured',
				suggest: 'Add a provider in Settings → Providers, then point your agent at it in Settings → Agents.',
				settings: 'providers'
			};
		}
		// Model not found.
		if (s.indexOf('model not found') >= 0 || s.indexOf('does not exist') >= 0 || s.indexOf('unknown model') >= 0) {
			return {
				title: 'Model not available',
				suggest: 'Check the model name in Settings → Agents. For local models, install it under Settings → Models.',
				settings: 'agents'
			};
		}
		// Local Ollama not reachable.
		if (s.indexOf('connection refused') >= 0 || s.indexOf('ollama') >= 0 || s.indexOf('eof') >= 0) {
			return {
				title: 'Local model is unavailable',
				suggest: 'The bundled local model service may not be running. Check Settings → Models.',
				settings: 'models'
			};
		}
		// Tool denied by policy.
		if (s.indexOf('not allowed for agent') >= 0 || s.indexOf('not allowed') >= 0) {
			return {
				title: 'A tool the agent tried to use is denied',
				suggest: 'Adjust this agent\'s allowed tools in Settings → Agents.',
				settings: 'agents'
			};
		}
		// Aborted / cancelled (cosmetic only).
		if (s.indexOf('aborted by user') >= 0 || s.indexOf('canceled') >= 0 || s.indexOf('cancelled') >= 0) {
			return { title: 'Run was cancelled', suggest: '' };
		}
		// Default — show the raw text but tag it.
		return { title: 'Something went wrong', suggest: raw };
	}

	function addError(msg) {
		messagesEl.classList.add('has-messages');
		var f = friendlyError(msg);
		var div = document.createElement('div');
		div.className = 'msg assistant';
		div.style.borderColor = 'var(--error)';
		// Mirror the addAssistantMsg dir=auto treatment so an RTL
		// error message renders right-to-left like the rest of the
		// bubbles. unicode-bidi: plaintext on .msg.assistant .content
		// (set in CSS) handles per-paragraph resolution.
		div.setAttribute('dir', 'auto');
		var html = '<div class="content" style="color:var(--error)">' +
			'<strong>' + escHtml(f.title) + '</strong>';
		if (f.suggest) {
			html += '<div style="margin-top:0.4rem; color:var(--text); font-size:0.85em;">' +
				escHtml(f.suggest) + '</div>';
		}
		if (f.settings) {
			html += '<div style="margin-top:0.4rem;">' +
				'<a href="/settings#' + escHtml(f.settings) + '" target="_blank" rel="noopener" style="color:var(--accent); text-decoration:none; font-size:0.8em;">' +
				'Open Settings &rarr;</a></div>';
		}
		// Always include the raw message in a folded details so power users
		// can still see what actually broke.
		html += '<details style="margin-top:0.5rem; color:var(--text-muted); font-size:0.75em;">' +
			'<summary style="cursor:pointer;">technical detail</summary>' +
			'<div style="margin-top:0.25rem; font-family:monospace; white-space:pre-wrap; word-break:break-all;">' +
			escHtml(msg) + '</div></details>';
		html += '</div>';
		div.innerHTML = html;
		messagesEl.appendChild(div);
		scrollToBottom();
	}

	function updateSendBtn() {
		if (sending) {
			sendBtn.style.display = 'none';
			stopBtn.style.display = 'block';
		} else {
			sendBtn.style.display = 'block';
			stopBtn.style.display = 'none';
			sendBtn.disabled = !ws || ws.readyState !== WebSocket.OPEN;
		}
	}

	function sendMessage() {
		var text = inputEl.value.trim();
		if (sending) return;
		if (!text && attachments.length === 0) return;
		if (!ws || ws.readyState !== WebSocket.OPEN) return;

		// Hand the attachment list to the user-message bubble so the
		// thumbnails appear immediately, then drop our reference so a
		// follow-up send doesn't re-include the same images.
		var sentAtts = attachments;
		attachments = [];

		addUserMsg(text, sentAtts);
		sending = true;
		updateSendBtn();
		msgId++;

		var params = {
			agentId: agentSelect.value,
			text: text,
			sessionKey: activeSessionKey
		};
		if (sentAtts.length > 0) {
			params.attachments = sentAtts.map(function(a) {
				return { mimeType: a.mimeType, data: a.dataB64, name: a.name };
			});
		}
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'chat.send',
			params: params,
			id: msgId
		}));

		// Auto-name a fresh session from its first user turn. Mirrors
		// the ChatGPT pattern: the sidebar entry should read "Auth
		// migration plan" rather than "ws_default" once you've started
		// a real conversation. Only fires when:
		//   - the active session has no friendly name yet, AND
		//   - the active session has zero prior entries (truly fresh),
		//   - the message text is non-empty (pure-attachment messages
		//     still keep the bare key — the default prompt isn't a
		//     useful title).
		// The rename RPC is fire-and-forget; if it fails the row
		// keeps its bare-key label, which is fine.
		if (text) {
			var current = null;
			for (var ci = 0; ci < sessionsCache.length; ci++) {
				if (sessionsCache[ci].key === activeSessionKey) {
					current = sessionsCache[ci];
					break;
				}
			}
			if (current && !current.name && (current.entryCount || 0) === 0) {
				var label = text.replace(/\s+/g, ' ').trim();
				if (label.length > 40) label = label.slice(0, 38).trim() + '…';
				if (label) renameSession(activeSessionKey, label);
			}
		}

		// Re-render the (now empty) chip strip and clear the input box.
		// Keep the object URLs alive — addUserMsg's <img> tags still
		// reference them — they'll be GC'd along with their bubble.
		renderAttachmentStrip();
		clearAttachError();
		inputEl.value = '';
		inputEl.style.height = 'auto';
	}

	sendBtn.addEventListener('click', sendMessage);

	stopBtn.addEventListener('click', function() {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({
			jsonrpc: '2.0',
			method: 'chat.abort',
			params: {},
			id: 'abort'
		}));
	});

	// IME-aware Enter handling. While an Input Method Editor is composing
	// a candidate (CJK/Vietnamese/Korean and similar), pressing Enter
	// commits the candidate — it must not also send the message. We
	// guard via two signals: the spec-level KeyboardEvent.isComposing,
	// and a compositionstart/compositionend tracked flag for the
	// browsers that don't set isComposing reliably (notably WebKit on
	// some macOS versions). keyCode 229 is the legacy IME pre-edit
	// fallback for the same case.
	var imeComposing = false;
	inputEl.addEventListener('compositionstart', function() { imeComposing = true; });
	inputEl.addEventListener('compositionend', function() {
		// Defer the unset by one frame so the Enter that committed the
		// candidate is observed as "still composing" by the keydown
		// listener below.
		setTimeout(function() { imeComposing = false; }, 0);
	});
	inputEl.addEventListener('keydown', function(e) {
		if (e.key !== 'Enter' || e.shiftKey) return;
		if (e.isComposing || e.keyCode === 229 || imeComposing) return;
		e.preventDefault();
		sendMessage();
	});

	// Auto-resize textarea
	inputEl.addEventListener('input', function() {
		this.style.height = 'auto';
		this.style.height = Math.min(this.scrollHeight, 150) + 'px';
	});

	// ----- Attachments -----------------------------------------------

	function setAttachError(msg) {
		attachErrorEl.textContent = msg;
		if (attachErrorTimer) clearTimeout(attachErrorTimer);
		attachErrorTimer = setTimeout(clearAttachError, 5000);
	}

	function clearAttachError() {
		attachErrorEl.textContent = '';
		if (attachErrorTimer) {
			clearTimeout(attachErrorTimer);
			attachErrorTimer = null;
		}
	}

	function formatBytes(n) {
		if (n < 1024) return n + ' B';
		if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
		return (n / (1024 * 1024)).toFixed(1) + ' MB';
	}

	// arrayBufferToBase64 streams the buffer through String.fromCharCode in
	// chunks. The naive one-shot apply() approach blows the JS argument-
	// stack on multi-MB images in some browsers, so we batch.
	function arrayBufferToBase64(buf) {
		var bytes = new Uint8Array(buf);
		var CHUNK = 0x8000;
		var parts = [];
		for (var i = 0; i < bytes.length; i += CHUNK) {
			parts.push(String.fromCharCode.apply(
				null, bytes.subarray(i, i + CHUNK)));
		}
		return btoa(parts.join(''));
	}

	function renderAttachmentStrip() {
		attachmentStrip.innerHTML = '';
		for (var i = 0; i < attachments.length; i++) {
			(function(idx, a) {
				var chip = document.createElement('span');
				chip.className = 'attachment-chip';
				chip.title = a.name + ' · ' + formatBytes(a.sizeBytes);
				chip.dataset.mime = a.mimeType;
				chip.dataset.kind = a.kind || 'other';

				if (a.kind === 'image' && a.objectUrl) {
					var thumb = document.createElement('img');
					thumb.className = 'attachment-thumb';
					thumb.src = a.objectUrl;
					thumb.alt = '';
					// If the bytes turn out to be unreadable as an image
					// (corrupted upload, wrong MIME on a binary file),
					// degrade gracefully to a generic IMG badge instead
					// of leaving the browser's broken-image glyph in
					// place.
					thumb.addEventListener('error', function() {
						var glyph = document.createElement('span');
						glyph.className = 'attachment-glyph';
						glyph.dataset.kind = 'image';
						glyph.textContent = 'IMG';
						thumb.replaceWith(glyph);
					});
					chip.appendChild(thumb);
				} else {
					// Generic file glyph — a small badge rather than a
					// raster thumbnail. Carries the kind so CSS can tint
					// per type (PDF / text / etc.).
					var glyph = document.createElement('span');
					glyph.className = 'attachment-glyph';
					glyph.dataset.kind = a.kind || 'other';
					var label;
					if (a.kind === 'doc') {
						label = a.mimeType === 'application/pdf' ? 'PDF' : 'DOC';
					} else if (a.kind === 'text') {
						label = 'TXT';
					} else {
						label = 'FILE';
					}
					glyph.textContent = label;
					chip.appendChild(glyph);
				}

				var name = document.createElement('span');
				name.className = 'attachment-name';
				name.textContent = a.name;
				chip.appendChild(name);

				var size = document.createElement('span');
				size.className = 'attachment-size';
				size.textContent = formatBytes(a.sizeBytes);
				chip.appendChild(size);

				var rm = document.createElement('button');
				rm.className = 'attachment-remove';
				rm.type = 'button';
				rm.setAttribute('aria-label', 'Remove ' + a.name);
				rm.innerHTML = '&times;';
				rm.addEventListener('click', function() { removeAttachment(idx); });
				chip.appendChild(rm);

				attachmentStrip.appendChild(chip);
			})(i, attachments[i]);
		}
	}

	function removeAttachment(idx) {
		var a = attachments[idx];
		if (a && a.objectUrl) URL.revokeObjectURL(a.objectUrl);
		attachments.splice(idx, 1);
		renderAttachmentStrip();
	}

	function addFiles(files) {
		clearAttachError();
		if (!files || files.length === 0) return;
		var added = 0;
		var rejections = [];
		for (var i = 0; i < files.length; i++) {
			var f = files[i];
			if (attachments.length >= MAX_ATTACHMENT_COUNT) {
				rejections.push('attachment limit (' + MAX_ATTACHMENT_COUNT + ') reached');
				break;
			}
			var mime = detectMime(f);
			if (!isAllowedMime(mime)) {
				rejections.push((f.name || 'file') + ': unsupported type (' + (f.type || 'unknown') + ')');
				continue;
			}
			var kind = attachmentKind(mime);
			// Audio gating — surface the "switch to Gemini" hint in the
			// UI before the upload even starts. Without this the user
			// gets a server-side rejection only after the WS round-trip,
			// which is slower feedback for an obvious mismatch.
			if (kind === 'audio' && !activeAgentCaps().nativeAudio) {
				rejections.push((f.name || 'audio') + ': audio uploads need a Gemini agent (current agent is not capable)');
				continue;
			}
			var cap = (kind === 'image') ? MAX_IMAGE_BYTES : MAX_DOC_BYTES;
			if (f.size > cap) {
				rejections.push((f.name || 'file') + ': too large (' + formatBytes(f.size) + ' > ' + formatBytes(cap) + ')');
				continue;
			}
			if (f.size === 0) {
				rejections.push((f.name || 'file') + ': empty file');
				continue;
			}
			added++;
			// Capture both the File reference AND the validated mime/kind
			// in the IIFE — both are var-scoped to the loop, so a naked
			// closure would race the next iteration and tag every chip
			// with the loop's final values.
			(function(file, fileMime, fileKind) {
				var reader = new FileReader();
				reader.onload = function() {
					try {
						var b64 = arrayBufferToBase64(reader.result);
						var entry = {
							name: file.name || 'attachment',
							mimeType: fileMime,
							kind: fileKind,
							sizeBytes: file.size,
							dataB64: b64
						};
						// Only image attachments need a thumbnail blob URL —
						// docs and text get a generic glyph instead, which
						// avoids spawning a blob URL per text upload.
						if (fileKind === 'image') {
							entry.objectUrl = URL.createObjectURL(file);
						}
						attachments.push(entry);
						renderAttachmentStrip();
						updateSendBtn();
					} catch (e) {
						setAttachError('Failed to read ' + (file.name || 'file') + ': ' + e.message);
					}
				};
				reader.onerror = function() {
					setAttachError('Failed to read ' + (file.name || 'file'));
				};
				reader.readAsArrayBuffer(file);
			})(f, mime, kind);
		}
		if (rejections.length > 0) {
			setAttachError(rejections.join(' · '));
		}
		// Note: no immediate strip render — each FileReader.onload triggers
		// one. That keeps chips appearing in load-completion order, which
		// matches what users expect when several large files are queued.
		if (added === 0 && rejections.length === 0) {
			// Nothing accepted, nothing rejected — likely a directory drop.
			setAttachError('No files attached');
		}
	}

	attachBtn.addEventListener('click', function() { filePicker.click(); });
	filePicker.addEventListener('change', function() {
		addFiles(filePicker.files);
		filePicker.value = ''; // allow re-selecting the same file
	});

	// Drag-drop on the input wrap. Listen on document so a drag anywhere
	// toggles the highlight, but only accept drops over the wrap so the
	// browser doesn't navigate away if the user misses the target.
	var dragDepth = 0;
	function hasFiles(e) {
		if (!e.dataTransfer) return false;
		var t = e.dataTransfer.types;
		if (!t) return false;
		for (var i = 0; i < t.length; i++) {
			if (t[i] === 'Files') return true;
		}
		return false;
	}
	document.addEventListener('dragenter', function(e) {
		if (!hasFiles(e)) return;
		e.preventDefault();
		dragDepth++;
		inputAreaWrap.classList.add('drag-over');
	});
	document.addEventListener('dragleave', function(e) {
		if (!hasFiles(e)) return;
		dragDepth = Math.max(0, dragDepth - 1);
		if (dragDepth === 0) inputAreaWrap.classList.remove('drag-over');
	});
	document.addEventListener('dragover', function(e) {
		if (!hasFiles(e)) return;
		e.preventDefault();
		e.dataTransfer.dropEffect = 'copy';
	});
	document.addEventListener('drop', function(e) {
		if (!hasFiles(e)) return;
		e.preventDefault();
		dragDepth = 0;
		inputAreaWrap.classList.remove('drag-over');
		addFiles(e.dataTransfer.files);
	});

	// Clipboard paste — pull image items out of the paste event.
	inputEl.addEventListener('paste', function(e) {
		var items = e.clipboardData && e.clipboardData.items;
		if (!items) return;
		var files = [];
		for (var i = 0; i < items.length; i++) {
			if (items[i].kind === 'file') {
				var f = items[i].getAsFile();
				if (f) files.push(f);
			}
		}
		if (files.length > 0) {
			e.preventDefault(); // suppress the file-name text the browser would otherwise paste
			addFiles(files);
		}
	});

	connect();
})();
</script>
</body>
</html>`
