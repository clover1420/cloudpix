package app

import "github.com/egoist/mygo/ui"

// 图标与 mygo-qyin 同一套画法：24 点网格、1.75 描边、跟随文字颜色。
func strokeIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

var (
	iconUpload   = strokeIcon(`<path d="M12 16V4"/><path d="m7 9 5-5 5 5"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/>`)
	iconImage    = strokeIcon(`<rect x="3" y="4" width="18" height="16" rx="2.5"/><circle cx="9" cy="10" r="1.8"/><path d="m21 15-4-4-9 9"/>`)
	iconHistory  = strokeIcon(`<path d="M3 12a9 9 0 1 0 3-6.7"/><path d="M3 4v5h5"/><path d="M12 8v4l3 2"/>`)
	iconSettings = strokeIcon(`<circle cx="12" cy="12" r="3"/><path d="M12 2v3"/><path d="M12 19v3"/><path d="M2 12h3"/><path d="M19 12h3"/><path d="m4.9 4.9 2.1 2.1"/><path d="m17 17 2.1 2.1"/><path d="m19.1 4.9-2.1 2.1"/><path d="m7 17-2.1 2.1"/>`)
	iconCloud    = strokeIcon(`<path d="M17.5 19a4.5 4.5 0 0 0 0-9 6 6 0 0 0-11.6 1.7A3.5 3.5 0 0 0 6.5 19z"/>`)
	iconCopy     = strokeIcon(`<rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>`)
	iconOpen     = strokeIcon(`<path d="M14 3h7v7"/><path d="M21 3 10 14"/><path d="M18 13v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h6"/>`)
	iconRetry    = strokeIcon(`<path d="M20 7v5h-5"/><path d="M20 12a8 8 0 1 0-2.3 5.7"/>`)
	iconX        = strokeIcon(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	iconCheck    = strokeIcon(`<path d="m5 12 4 4L19 6"/>`)
)
