package main

// screen_settings.go

import "strconv"

type overlayPixelDimensions struct {
	glyphW  int
	glyphH  int
	ascent  int
	lineGap int
	buttonW int
	valueW  int
	gap     int
	labelW  int
}

func (g *Game) overlayMetrics() overlayPixelDimensions {
	font := g.font
	scale := font.scale
	gw := int(float32(font.face.Advance) * scale)
	gh := int(float32(font.face.Height) * scale)

	return overlayPixelDimensions{
		glyphW:  gw,
		glyphH:  gh,
		ascent:  int(float32(font.face.Ascent) * scale),
		lineGap: gh * 3 / 2,
		buttonW: gw * 2,
		valueW:  gw * 3,
		gap:     gw,
		labelW:  gw * 5,
	}
}

type pixelSpaceRect struct {
	x int
	y int
	w int
	h int
}

func overlayBox(m overlayPixelDimensions, x, baselineY int) pixelSpaceRect {
	return pixelSpaceRect{
		x: x,
		y: baselineY - m.ascent,
		w: m.buttonW,
		h: m.glyphH,
	}
}

type settingsRow struct {
	labelX, baselineY int
	valueCenterX      int
	prev, next        pixelSpaceRect
}

func controlRow(m overlayPixelDimensions, leftX, baselineY int) settingsRow {
	prevX := leftX + m.labelW
	valueX := prevX + m.buttonW + m.gap
	nextX := valueX + m.valueW + m.gap

	return settingsRow{
		labelX:       leftX,
		baselineY:    baselineY,
		valueCenterX: valueX + m.valueW/2,
		prev:         overlayBox(m, prevX, baselineY),
		next:         overlayBox(m, nextX, baselineY),
	}
}

type settingsWidgets struct {
	centerX       int
	titleY, hintY int

	fps, tps settingsRow

	noLimitY      int
	noLimitLabelX int
	noLimitBox    pixelSpaceRect
}

func (g *Game) settingsLayout() settingsWidgets {
	m := g.overlayMetrics()

	const (
		lineTitle   = 0
		lineHint    = 1
		_           = 2
		lineFPS     = 3
		lineNoLimit = 4
		_           = 5
		lineTPS     = 6
		lineCount   = 7
	)

	top := WindowSize/2 - m.lineGap*(lineCount-1)/2
	lineY := func(slot int) int {
		return top + slot*m.lineGap
	}

	rowX := WindowSize / 3

	noLimitW := m.buttonW + m.gap + len("No Limit")*m.glyphW
	noLimitX := (WindowSize - noLimitW) / 2
	noLimitY := lineY(lineNoLimit)

	return settingsWidgets{
		centerX: WindowSize / 2,
		titleY:  lineY(lineTitle),
		hintY:   lineY(lineHint),

		fps: controlRow(m, rowX, lineY(lineFPS)),
		tps: controlRow(m, rowX, lineY(lineTPS)),

		noLimitY:      noLimitY,
		noLimitLabelX: noLimitX + m.buttonW + m.gap,
		noLimitBox:    overlayBox(m, noLimitX, noLimitY),
	}
}

var pacingPresets = [9]int{30, 45, 60, 90, 120, 180, 240, 360, 480}

const (
	minPreset    = 30
	maxPreset    = 480
	defaultRate  = 120
	defaultIndex = 4
)

var (
	fpsPresetIndex = defaultIndex
	tpsPresetIndex = defaultIndex
	fpsNoLimit     bool
)

func fpsFieldValue() int {
	if fpsNoLimit {
		return 0
	}
	return pacingPresets[fpsPresetIndex]
}

func tpsFieldValue() int { return pacingPresets[tpsPresetIndex] }

func (g *Game) drawSettingsOverlay() {
	gfx := g.gfx
	font := g.font
	w := g.settingsLayout()

	// Draw grouped by draw state, not by control -- performance due to flushes

	gfx.FillRect(0, 0, WindowSize, WindowSize, ColorWhite)

	g.strokeSettingsBox(w.fps.prev)
	g.strokeSettingsBox(w.fps.next)
	g.strokeSettingsBox(w.noLimitBox)
	g.strokeSettingsBox(w.tps.prev)
	g.strokeSettingsBox(w.tps.next)

	font.DrawTextCentered(gfx, "Settings", w.centerX, w.titleY, ColorBlack)
	font.DrawTextCentered(gfx, "ESC to close", w.centerX, w.hintY, ColorBlack)
	g.drawRowText("FPS:", w.fps, fpsFieldValue())
	g.drawRowText("TPS:", w.tps, tpsFieldValue())
	drawGlyphs(font, gfx, "No Limit", w.noLimitLabelX, w.noLimitY, ColorBlack, font.scale)
	if fpsNoLimit {
		font.DrawTextCentered(gfx, "X", w.noLimitBox.x+w.noLimitBox.w/2, w.noLimitY, ColorBlack)
	}
}

const settingsBorder = 2

func (g *Game) strokeSettingsBox(box pixelSpaceRect) {
	gfx := g.gfx
	gfx.StrokeRect(float32(box.x), float32(box.y), float32(box.w), float32(box.h),
		settingsBorder, ColorBlack)
}

func (g *Game) drawRowText(label string, row settingsRow, value int) {
	gfx := g.gfx
	font := g.font
	drawGlyphs(font, gfx, label, row.labelX, row.baselineY, ColorBlack, font.scale)
	font.DrawTextCentered(gfx, strconv.Itoa(value), row.valueCenterX, row.baselineY, ColorBlack)
	font.DrawTextCentered(gfx, "<", row.prev.x+row.prev.w/2, row.baselineY, ColorBlack)
	font.DrawTextCentered(gfx, ">", row.next.x+row.next.w/2, row.baselineY, ColorBlack)
}

func (g *Game) updateSettings() {
	in := g.input
	px, py, ok := in.LeftPressEvent()
	if !ok {
		return
	}

	w := g.settingsLayout()

	switch {
	case pxRectContains(w.fps.prev, px, py):
		if !fpsNoLimit {
			fpsPresetIndex = movePresetWithWrapAround(fpsPresetIndex, -1)
		}

	case pxRectContains(w.fps.next, px, py):
		if !fpsNoLimit {
			fpsPresetIndex = movePresetWithWrapAround(fpsPresetIndex, +1)
		}

	case pxRectContains(w.noLimitBox, px, py):
		if fpsNoLimit {
			fpsNoLimit = false
			fpsPresetIndex = defaultIndex
		} else {
			fpsNoLimit = true
		}

	case pxRectContains(w.tps.prev, px, py):
		tpsPresetIndex = movePresetWithWrapAround(tpsPresetIndex, -1)

	case pxRectContains(w.tps.next, px, py):
		tpsPresetIndex = movePresetWithWrapAround(tpsPresetIndex, +1)
	}

	if fpsNoLimit {
		maxFPS = 0
	} else {
		maxFPS = pacingPresets[fpsPresetIndex]
	}
	maxTPS = pacingPresets[tpsPresetIndex]
	requestPacingResync()
}

func pxRectContains(r pixelSpaceRect, px, py int) bool {
	return px >= r.x &&
		px < r.x+r.w &&
		py >= r.y &&
		py < r.y+r.h
}

func movePresetWithWrapAround(i, dir int) int {
	n := len(pacingPresets)
	i += dir
	if i < 0 {
		return n - 1
	}
	if i >= n {
		return 0
	}
	return i
}
