package app

import (
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// This file holds the reader's overlay look, after comimi's viewer: floating
// glass cards over the page rather than a bar across it. The reader keeps its
// own light and dark palette because the page behind the cards is the reader's
// background, not the app's theme.

// readerLook is the palette of one reader frame: the paper the pages sit on and
// the glass, ink and hairlines of the controls over it.
type readerLook struct {
	paper       ui.Color
	glass       ui.Color
	glassStrong ui.Color
	fg          ui.Color
	muted       ui.Color
	soft        ui.Color
	line        ui.Color
	surface     ui.Color
	surface2    ui.Color
	white       ui.Color
	shadow      ui.Color
}

// readerLookOf returns comimi's light or dark palette. The reader's background
// setting picks the palette too, so a black page gets dark glass and a white
// page light glass.
func readerLookOf(r *Reader) readerLook {
	if r.settings.dark {
		return readerLook{
			paper:       ui.RGB(28, 28, 30),
			glass:       ui.RGBA(40, 40, 43, 0.88),
			glassStrong: ui.RGBA(30, 30, 33, 0.95),
			fg:          ui.RGB(237, 237, 237),
			muted:       ui.RGB(179, 179, 179),
			soft:        ui.RGB(126, 126, 130),
			line:        ui.RGB(63, 63, 67),
			surface:     ui.RGB(44, 44, 46),
			surface2:    ui.RGB(58, 58, 60),
			white:       ui.RGB(255, 255, 255),
			shadow:      ui.RGBA(0, 0, 0, 0.55),
		}
	}
	return readerLook{
		paper:       ui.RGB(255, 255, 255),
		glass:       ui.RGBA(255, 255, 255, 0.9),
		glassStrong: ui.RGBA(255, 255, 255, 0.96),
		fg:          ui.RGB(51, 51, 51),
		muted:       ui.RGB(102, 102, 102),
		soft:        ui.RGB(153, 153, 153),
		line:        ui.RGB(224, 224, 224),
		surface:     ui.RGB(247, 247, 247),
		surface2:    ui.RGB(238, 238, 238),
		white:       ui.RGB(255, 255, 255),
		shadow:      ui.RGBA(0, 0, 0, 0.14),
	}
}

// The reader's marks, drawn as line icons so they take the ink of whatever
// card they sit on.
var (
	readerIconMenu   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M4 7h16M4 12h16M4 17h16"/></svg>`))
	readerIconClose  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="m6 6 12 12M18 6 6 18"/></svg>`))
	readerIconBack   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 12H5"/><path d="m11 6-6 6 6 6"/></svg>`))
	readerIconNext   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7"/></svg>`))
	readerIconPrev   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7"/></svg>`))
	readerIconPlay   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M8 4.8v14.4L19 12 8 4.8z"/></svg>`))
	readerIconPause  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" stroke="none"><rect x="6.5" y="5" width="3.6" height="14" rx="1"/><rect x="13.9" y="5" width="3.6" height="14" rx="1"/></svg>`))
	readerIconGear   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3.2"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/></svg>`))
	readerIconExpand = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M9 4H4v5"/><path d="m4 4 6 6"/><path d="M15 20h5v-5"/><path d="m20 20-6-6"/></svg>`))
	readerIconWindow = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="3.5" y="5" width="17" height="14" rx="2.5"/><path d="M3.5 9h17"/></svg>`))
	readerIconSingle = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="7" y="4" width="10" height="16" rx="1.5"/><path d="M10.5 4v16"/></svg>`))
	readerIconSpread = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><rect x="2.5" y="4" width="8.5" height="16" rx="1.5"/><rect x="13" y="4" width="8.5" height="16" rx="1.5"/></svg>`))
	readerIconMinus  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 12h12"/></svg>`))
	readerIconPlus   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 6v12M6 12h12"/></svg>`))
	readerIconCheck  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12.5 4.5 4.5L19 7"/></svg>`))
)

// readerInset is the margin the floating controls keep from the window edge:
// comimi's 24 points, narrowed on a small window so the dock still fits.
func readerInset(w float32) float32 {
	if w < 720 {
		return 12
	}
	return 24
}

// readerGlassCard draws one of the overlay's cards: the rounded translucent
// pane with the shadow that lifts it off the page.
func readerGlassCard(c *ui.Context, look readerLook, radius float32) ui.Element {
	return ui.Box(c).Radius(radius).Background(look.glass).
		Shadow(0, 4, 18, 0, look.shadow)
}

// readerIconButton is a quiet square icon button on a card. The label is what
// a screen reader (and the tests) call it; the icon is what is drawn.
func (a *App) readerIconButton(c *ui.Context, look readerLook, mark *ui.SVG, label string, size, glyph float32, color ui.Color, run func()) ui.Element {
	btn := ui.Box(c).Size(size, size).Radius(10).Center().Shrink(0).
		Focusable().Label(label).Tooltip(label)
	btn.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputPointerDown || run == nil {
			return false
		}
		a.markActivity()
		run()
		return true
	})
	if btn.Hovered() {
		btn.Background(look.surface2)
	}
	btn.Children(func() {
		ui.Icon(c, mark).Size(glyph, glyph).TextColor(color)
	})
	return btn
}

// readerSegmentButton is one half of a two-way switch: the chosen side takes
// the switch's indicator, the other stays flat until hovered. comimi draws its
// page mode and view switches this way.
func (a *App) readerSegmentButton(c *ui.Context, look readerLook, width, height float32, mark *ui.SVG, label string, selected bool, run func()) {
	color := look.muted
	if selected {
		color = look.white
	}
	btn := ui.Box(c).Size(width, height).Radius(12).Center().
		Focusable().Label(label).Tooltip(label)
	if !selected {
		btn.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputPointerDown {
				return false
			}
			a.markActivity()
			if run != nil {
				run()
			}
			return true
		})
		if btn.Hovered() {
			color = look.fg
		}
	}
	btn.Children(func() {
		ui.Icon(c, mark).Size(18, 18).TextColor(color)
	})
}

// readerSegments wraps a pair of segment buttons in comimi's 2-column track,
// with the chosen tile sliding between the halves as a pose: the renderer
// carries it without rebuilding the switch.
func readerSegments(c *ui.Context, look readerLook, selected int, fn func()) ui.Element {
	row := ui.Row(c).Width(84).Height(36).Radius(12).Background(look.surface2)
	row.Children(func() {
		ui.Box(c).Width(42).Height(36).Radius(12).Background(look.muted).
			Absolute().Top(0).Left(0).
			AnimatePose(ui.Pose{X: float32(selected) * 42}, ui.Snappy)
		fn()
	})
	return row
}

// readerLabel is a small muted caption above a group of settings.
func readerLabel(c *ui.Context, look readerLook, text string) {
	ui.Text(c, text).FontSize(12).TextColor(look.fg)
}

// readerSliderRow is the settings card's range control, drawn in the reader's
// palette: a hairline track, the value filled to the knob, and the knob itself.
// A press on the track lands the value there, as a slider does.
func (a *App) readerSliderRow(c *ui.Context, look readerLook, value *float64, lo, hi, step float32, width float32, changed func(float32)) {
	clampValue := func(v float64) float64 {
		if v < float64(lo) {
			v = float64(lo)
		}
		if v > float64(hi) {
			v = float64(hi)
		}
		if step > 0 {
			v = float64(lo) + float64(int((v-float64(lo))/float64(step)+0.5))*float64(step)
		}
		return v
	}
	slider := ui.Box(c).FillWidth().Height(18).Focusable().
		Label(i18n.T("Auto page turn"))
	slider.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputPointerDown || width <= 0 {
			return false
		}
		a.markActivity()
		ratio := clampFloat(ev.X/width, 0, 1)
		v := clampValue(float64(lo) + float64(ratio)*(float64(hi)-float64(lo)))
		if v != *value {
			*value = v
			if changed != nil {
				changed(float32(v))
			}
		}
		return true
	})
	slider.Children(func() {
		fraction := float32(0)
		if hi > lo {
			fraction = clampFloat(float32((*value-float64(lo))/(float64(hi)-float64(lo))), 0, 1)
		}
		ui.Box(c).FillWidth().Height(6).Radius(999).Absolute().Top(6).Left(0).
			Background(look.line)
		if fraction > 0 {
			ui.Box(c).Height(6).Radius(999).Absolute().Top(6).Left(0).
				Width(width * fraction).Background(look.muted)
		}
		knobX := fraction*width - 8
		if knobX < 0 {
			knobX = 0
		}
		if knobX > width-16 {
			knobX = width - 16
		}
		ui.Box(c).Size(16, 16).Radius(999).Absolute().Top(1).Left(knobX).
			Background(look.muted).Shadow(0, 1, 4, 0, look.shadow)
	})
}

// readerCheckRow is one checkbox row in the settings card, drawn in the
// reader's own palette rather than the app theme's.
func (a *App) readerCheckRow(c *ui.Context, look readerLook, label string, checked bool, toggle func()) {
	row := ui.Box(c).FillWidth().Height(24).Focusable().Label(label)
	row.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputPointerDown {
			return false
		}
		a.markActivity()
		if toggle != nil {
			toggle()
		}
		return true
	})
	if row.Hovered() {
		row.Background(look.surface)
	}
	row.Children(func() {
		ui.Row(c).Fill().Gap(8).AlignItems(ui.Center).Children(func() {
			box := ui.Box(c).Size(18, 18).Radius(6).Center().Shrink(0).
				Background(look.surface2).Border(1, look.line)
			if checked {
				box.Background(look.muted).Border(0, ui.Transparent)
			}
			box.Children(func() {
				if checked {
					ui.Icon(c, readerIconCheck).Size(12, 12).TextColor(look.white)
				}
			})
			ui.Text(c, label).FontSize(13).TextColor(look.fg).Grow(1)
		})
	})
}

// readerChoiceRow is the settings card's two-way choice: two equal buttons
// side by side, the chosen one filled with the reader's ink.
func (a *App) readerChoiceRow(c *ui.Context, look readerLook, left, right string, leftChosen bool, chooseLeft func(), chooseRight func()) {
	row := ui.Row(c).FillWidth().Height(34).Gap(4).Padding(3).Radius(10).
		Background(look.surface2)
	row.Children(func() {
		mk := func(label string, chosen bool, run func()) {
			bg := ui.Transparent
			color := look.muted
			if chosen {
				bg = look.muted
				color = look.white
			}
			btn := ui.Box(c).BasisPercent(50).Height(28).Radius(8).Center().Background(bg).
				Focusable().Label(label)
			if !chosen {
				btn.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind != ui.InputPointerDown {
						return false
					}
					a.markActivity()
					if run != nil {
						run()
					}
					return true
				})
			}
			btn.Children(func() {
				ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(color).SingleLine()
			})
		}
		mk(left, leftChosen, chooseLeft)
		mk(right, !leftChosen, chooseRight)
	})
}
