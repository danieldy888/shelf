//go:build darwin

package main

// Every string the interface says lives in the catalogs of locales/, in the
// source language zh-Hans and in English. Views read the messages of the
// language of their window, which mygo redraws when it changes; native
// surfaces (the tray, its menus and dialogs) read the language the shelf
// shows, which OnLocaleChanged keeps up to date.

import (
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"shelf/locales"
)

// loc returns the messages of the language of c's window. A view that is
// built again every frame follows a language change with no restart; a
// headless Tester's SetLocale drives what a test reads.
func loc(c *ui.Context) locales.Messages { return locales.In(c.Locale()) }

// tloc returns the messages of the language of c's window, formatted the way
// the user's regional settings say: dates and times follow the 12- or
// 24-hour clock and the first day of the week, whatever the language. A
// headless Tester has no region, so a test sees its language's CLDR default.
func tloc(c *ui.Context) locales.Messages {
	tag, r := c.Locale(), c.Region()
	if tag == "" || r == (i18n.Region{}) {
		return locales.In(tag)
	}
	return locales.In(r.FormatLocale(tag))
}

// stampIn is a moment in a clipboard row's title: the compact 「01-02 15:04」
// the source language has always shown, or the region's short date and time.
func stampIn(locale string, r i18n.Region, t time.Time) string {
	if strings.HasPrefix(locale, "zh") {
		return t.Format("01-02 15:04")
	}
	return i18n.FormatDate(r.FormatLocale(locale), t, i18n.DateOptions{
		Date: i18n.ShortStyle, Time: i18n.ShortStyle, HourCycle: r.HourCycle,
	})
}

// appLoc returns the messages of the language the shelf shows, for the
// native surfaces built outside a view: the tray, its menus and dialogs.
// A headless shelf keeps the source language until a test sets a.locale.
func (a *shelf) appLoc() locales.Messages { return locales.In(a.locale) }

// languageTag returns tag when it names one of the app's languages, else ""
// (follow the system): settings.json is user-editable, and mygo.App.SetLocale
// panics on a tag that names no language.
func languageTag(tag string) string {
	switch tag {
	case "zh-Hans", "en":
		return tag
	}
	return ""
}

// localeChanged follows the app's language as mygo changes it: the native
// surfaces that are built once — window titles and the tray's tooltip — say
// the new language; views redraw themselves because they are built per
// frame. It runs on the main thread, also when the app is not ready yet, so
// every window is checked.
func (a *shelf) localeChanged(tag string) {
	a.locale = tag
	m := a.appLoc()
	if a.tray != nil {
		a.tray.SetToolTip(m.AppTrayTooltip())
	}
	a.retitleWindows()
}

// retitleWindows gives the windows that outlive a language change their
// title in the language the shelf now says.
func (a *shelf) retitleWindows() {
	m := a.appLoc()
	if a.settingsWin != nil {
		a.settingsWin.SetTitle(m.WindowSettings())
	}
	if a.basket.win != nil {
		a.basket.win.SetTitle(m.WindowBasket())
	}
	for _, p := range a.pins {
		if p.win != nil {
			p.win.SetTitle(m.WindowPins())
		}
	}
	for _, isl := range a.idleIslands {
		if isl.win != nil {
			isl.win.SetTitle(m.WindowIsland())
		}
	}
	if a.rn != nil && a.rn.win != nil {
		a.rn.win.SetTitle(a.renameTitle(len(a.rn.items)))
	}
}
