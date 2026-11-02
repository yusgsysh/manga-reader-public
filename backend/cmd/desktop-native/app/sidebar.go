package app

import (
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// sidebarWidthCollapsed is main's collapsed rail: 4.5rem.
const sidebarWidthCollapsed float32 = 72

// railWidth is the room the navigation rail takes now: main's 15rem when
// open, 4.5rem folded.
func (a *App) railWidth() float32 {
	if a.sidebarCollapsed {
		return sidebarWidthCollapsed
	}
	return sidebarWidth
}

// sidebar is the navigation rail, as main's DesktopSidebar: a book mark and
// the app's name, the page links with the chosen one in the accent colour, a
// divider before the personal lists, and the theme switch at the foot. The
// rail folds to icons with the control beside the name.
func (a *App) sidebar(c *ui.Context, t *ui.Theme) {
	w := a.railWidth()
	ui.Column(c).Width(w).Shrink(0).Background(t.Background).
		BorderEndWidth(1).BorderColor(t.Border).Children(func() {
		a.sidebarBrand(c, t)
		ui.Column(c).Gap(4).Padding(8, 12).Grow(1).Shrink(0).Children(func() {
			a.navItem(c, "/", iconSVG[iconHome], i18n.T("Home"))
			a.navItem(c, "/watched", iconSVG[iconWatched], i18n.T("Watched"))
			a.navItem(c, "/popular", iconSVG[iconPopular], i18n.T("Popular"))
			a.navItem(c, "/search", iconSVG[iconSearch], i18n.T("Search"))
			a.navItem(c, "/bookshelf", iconSVG[iconBookshelf], i18n.T("Bookshelf"))
			ui.Divider(c).Margin(8, 0, 8, 0)
			a.navItem(c, "/recently-read", iconSVG[iconRecent], i18n.T("Recently read"))
			a.navItem(c, "/downloads", iconSVG[iconDownloads], i18n.T("Downloads"))
			a.navItem(c, "/tag-settings", iconSVG[iconTags], i18n.T("Tag settings"))
			a.navItem(c, "/settings", iconSVG[iconSettings], i18n.T("Settings"))
		})
		a.themeSwitch(c, t)
	})
}

// sidebarBrand is the rail's head: the book mark, the app's name and the
// control that folds the rail. Collapsed, the mark makes room for the control.
func (a *App) sidebarBrand(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Height(56).PaddingX(12).AlignItems(ui.Center).Gap(8).Shrink(0).Children(func() {
		if !a.sidebarCollapsed {
			ui.Icon(c, bookSVG).Size(24, 24).TextColor(t.Accent).Shrink(0)
			ui.Text(c, "Manga Reader").FontSize(15).FontWeight(600).Grow(1).SingleLine()
		} else {
			ui.Box(c).Grow(1)
		}
		mark := chevronsLeftSVG
		label := i18n.T("Collapse sidebar")
		if a.sidebarCollapsed {
			mark, label = chevronsRightSVG, i18n.T("Expand sidebar")
		}
		btn := ui.Box(c).Size(36, 36).Radius(8).Center().Shrink(0).Focusable().
			Label(label).Tooltip(label)
		if btn.Hovered() {
			btn.Background(t.SurfaceHover)
		}
		btn.Children(func() {
			ui.Icon(c, mark).Size(20, 20).TextColor(t.TextMuted)
		})
		if btn.Clicked() {
			a.sidebarCollapsed = !a.sidebarCollapsed
		}
	})
}

// navItem is one page link: 36 points high, a 12-point corner, the icon and
// the name. The chosen page is the accent pill main shows, and a hovered one
// takes the hush of the theme's hover surface.
func (a *App) navItem(c *ui.Context, path string, mark *ui.SVG, label string) {
	t := c.Theme()
	selected := a.selected == path
	item := ui.Row(c).Height(36).Radius(12).AlignItems(ui.Center).
		Focusable().Label(label).Cursor(ui.CursorPointer)
	if a.sidebarCollapsed {
		item.Justify(ui.Center).PaddingX(0)
	} else {
		item.Gap(12).PaddingX(12)
	}
	bg := ui.Color{}
	switch {
	case selected:
		bg = t.Accent
	case item.Hovered():
		bg = t.SurfaceHover
	}
	// The pill fades between its states instead of blinking on and off. A
	// colour animation runs on its own; typing a new page does not wait for
	// the old one to finish.
	item.Background(item.AnimateColor("nav-tint", bg, tintFade))
	if a.sidebarCollapsed {
		item.Tooltip(label)
	}
	iconColor, textColor := t.TextMuted, t.TextMuted
	if selected {
		iconColor, textColor = t.AccentText, t.AccentText
	}
	iconColor = item.AnimateColor("nav-icon", iconColor, tintFade)
	textColor = item.AnimateColor("nav-text", textColor, tintFade)
	item.Children(func() {
		ui.Icon(c, mark).Size(20, 20).TextColor(iconColor)
		if !a.sidebarCollapsed {
			ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(textColor).Grow(1).SingleLine()
		}
	})
	if item.Clicked() {
		a.router.Push(path)
	}
}

// themeSwitch is the foot of the rail: light, dark and follow-system as one
// segmented control, as main shows them. Collapsed, it stacks.
func (a *App) themeSwitch(c *ui.Context, t *ui.Theme) {
	if a.appearance == "" {
		a.appearance = "system"
	}
	options := []struct {
		value string
		mark  *ui.SVG
		label string
	}{
		{"light", sunSVG, i18n.T("Light")},
		{"dark", moonSVG, i18n.T("Dark")},
		{"system", displaySVG, i18n.T("Follow system")},
	}
	outer := ui.Column(c).Padding(12).Shrink(0)
	if a.sidebarCollapsed {
		outer.Children(func() {
			box := ui.Column(c).Padding(3).Gap(2).Radius(10).Background(t.Surface)
			box.Children(func() {
				for _, opt := range options {
					a.themeCell(c, t, opt.value, opt.mark, opt.label, true)
				}
			})
		})
		return
	}
	outer.Children(func() {
		box := ui.Row(c).Height(40).Padding(3).Radius(10).Background(t.Surface)
		box.Children(func() {
			for _, opt := range options {
				a.themeCell(c, t, opt.value, opt.mark, opt.label, false)
			}
		})
	})
}

// themeCell is one segment of the theme switch. The chosen one sits on the
// window's own surface with the accent mark, as main's segmented control.
func (a *App) themeCell(c *ui.Context, t *ui.Theme, value string, mark *ui.SVG, label string, stacked bool) {
	active := a.appearance == value
	cell := ui.Box(c).Radius(8).Center().Cursor(ui.CursorPointer).Focusable().
		Label(label).Tooltip(label)
	if stacked {
		cell.Size(42, 36)
	} else {
		cell.Grow(1).Height(34)
	}
	bg := ui.Color{}
	switch {
	case active:
		bg = t.Background
		cell.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
	case cell.Hovered():
		bg = t.SurfaceHover
	}
	cell.Background(cell.AnimateColor("theme-tint", bg, tintFade))
	cell.Children(func() {
		color := t.TextMuted
		if active {
			color = t.Accent
		}
		color = cell.AnimateColor("theme-icon", color, tintFade)
		ui.Icon(c, mark).Size(16, 16).TextColor(color)
	})
	if cell.Clicked() && !active {
		a.saveAppearance(value)
	}
}

// chevronsLeftSVG/chevronsRightSVG fold and unfold the rail, as main's pair.
var (
	chevronsLeftSVG  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m11 6-6 6 6 6"/><path d="m19 6-6 6 6 6"/></svg>`))
	chevronsRightSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m13 6 6 6-6 6"/><path d="m5 6 6 6-6 6"/></svg>`))
	sunSVG           = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4.2"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.2 5.2l1.4 1.4M17.4 17.4l1.4 1.4M18.8 5.2l-1.4 1.4M6.6 17.4l-1.4 1.4"/></svg>`))
	moonSVG          = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5z"/></svg>`))
	displaySVG       = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M9 20h6M12 16.5V20"/></svg>`))
)
