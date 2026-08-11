package main

// game.go

import (
	"github.com/go-gl/glfw/v3.3/glfw"
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------------------------------------------------

type CellCoords struct {
	x, y int
}

type CellRect struct {
	topLeft     CellCoords
	bottomRight CellCoords
}

type Game struct {
	grid              [GridSize][GridSize]int
	clues             []clueCell
	rects             []CellRect
	dragStart         CellCoords
	dragging          bool
	solved            bool
	waitingForRelease bool
	settingsOpen      bool
	staticLayer       *RenderTarget

	gfx   *Renderer
	font  *FontAtlas
	input *Input

	debugText []byte
	showDebug bool
}

func NewGame(gfx *Renderer, font *FontAtlas, in *Input, showDebug bool) *Game {
	assert(gfx != nil && font != nil && in != nil,
		"game: NewGame needs a renderer, a font atlas and an input")

	g := &Game{gfx: gfx, font: font, input: in}
	g.rects = make([]CellRect, 0, maxClues)
	//noinspection GoBoolExpressions
	if debugOverlayEnabled {
		g.showDebug = showDebug
		g.debugText = make([]byte, 0, 128)
		g.setDebugStats()
	}
	g.startNewPuzzle()
	return g
}

func (g *Game) startNewPuzzle() {
	g.grid, g.clues = newPuzzle()

	totalClueArea := 0
	for _, c := range g.clues {
		assert(c.row >= 0 && c.row < GridSize && c.col >= 0 && c.col < GridSize,
			"game: newPuzzle returned a clue outside the board")
		assert(g.grid[c.row][c.col] == c.area,
			"game: clue disagrees with the grid cell it names")
		totalClueArea += c.area
	}
	assert(totalClueArea == GridSize*GridSize,
		"game: newPuzzle's clue areas do not tile the board -- the puzzle is unwinnable")

	cluedCells := 0
	for y := 0; y < GridSize; y++ {
		for x := 0; x < GridSize; x++ {
			if g.grid[y][x] != 0 {
				cluedCells++
			}
		}
	}
	assert(cluedCells == len(g.clues),
		"game: the grid shows a clue that g.clues does not list")

	g.rects = g.rects[:0]
	g.solved = false
	g.dragging = false
	g.waitingForRelease = true
	g.cacheStaticLayer()
	requestPacingResync()
}

func (g *Game) Update() {
	if g.settingsOpen {
		g.updateSettings()
		return
	}
	if g.solved {
		g.updateSuccess()
		return
	}
	g.updatePlay()
}

// ---------------------------------------------------------------------------------------------------------------------

func (g *Game) Draw() {
	gfx := g.gfx

	switch {
	case g.settingsOpen:
		// screen_settings.go
		g.drawSettingsOverlay()

	case g.solved:
		// screen_success.go
		g.drawSuccessOverlay()

	default:
		// screen_play.go
		g.drawPlay()
	}

	//noinspection GoBoolExpressions
	if debugOverlayEnabled {
		g.drawDebugOverlay()
	}

	gfx.Flush()
}

func (g *Game) drawDebugOverlay() {
	if !g.showDebug {
		return
	}
	gfx := g.gfx
	font := g.font
	const dbgScale float32 = 1
	posX := 5
	posY := 5 + int(float32(font.face.Ascent)*dbgScale)
	drawGlyphs(font, gfx, g.debugText, posX, posY, ColorBlack, dbgScale)
}

// also used in main.go
func (g *Game) setDebugStats() {
	b := g.debugText[:0]

	b = append(b, "WORK: "...)
	b = appendMillis(b, pacing.workOverrunWorst)
	b = append(b, "\nWAIT: "...)
	b = appendMillis(b, pacing.waitOverrunWorst)
	b = append(b, "\nSPIN: "...)
	b = appendMillis(b, spinMarginValue())
	b = append(b, "\nBLOCK: "...)
	b = appendSeconds(b, pacing.pollBlockedWorst)
	b = append(b, "\nSWAP: "...)
	b = appendMillis(b, pacing.swapBlockedWorst)

	in := g.input
	if drops := in.queue.drops; drops > 0 {
		b = append(b, "\nINPUT DROPS: "...)
		b = strconv.AppendInt(b, int64(drops), 10)
	}
	if dupes := in.queue.dupes; dupes > 0 {
		b = append(b, "\nINPUT DUPES: "...)
		b = strconv.AppendInt(b, int64(dupes), 10)
	}
	if pacing.framesDropped > 0 {
		b = append(b, "\nFRAME DROPS: "...)
		b = strconv.AppendInt(b, int64(pacing.framesDropped), 10)
	}

	g.debugText = b
}

func appendMillis(dst []byte, d time.Duration) []byte {
	dst = strconv.AppendFloat(dst, float64(d)/float64(time.Millisecond), 'f', 2, 64)
	return append(dst, 'm', 's')
}

func appendSeconds(dst []byte, d time.Duration) []byte {
	dst = strconv.AppendFloat(dst, d.Seconds(), 'f', 2, 64)
	return append(dst, 's')
}

// ---------------------------------------------------------------------------------------------------------------------

const maxQueuedEvents = 64

type mouseEvent struct {
	pressed bool
	x, y    int
}

// Deliberately separate from Input
type buttonQueue struct {
	events [maxQueuedEvents]mouseEvent
	head   int
	length int

	lastPushed    bool
	hasLastPushed bool

	drops, dupes int
}

func (q *buttonQueue) push(ev mouseEvent) {
	if q.hasLastPushed && q.lastPushed == ev.pressed {
		q.dupes++
	}
	q.lastPushed, q.hasLastPushed = ev.pressed, true

	if q.length == maxQueuedEvents {
		q.head = (q.head + 1) % maxQueuedEvents
		q.length--
		q.drops++
	}

	q.events[(q.head+q.length)%maxQueuedEvents] = ev
	q.length++

	assert(q.length <= maxQueuedEvents, "input: ring length exceeds capacity")
	assert(q.head < maxQueuedEvents, "input: ring head out of range")
}

func (q *buttonQueue) pop() (mouseEvent, bool) {
	if q.length == 0 {
		return mouseEvent{}, false
	}

	ev := q.events[q.head]
	q.head = (q.head + 1) % maxQueuedEvents
	q.length--
	return ev, true
}

type Input struct {
	win *glfw.Window

	prevLeft bool
	currLeft bool

	cursorX, cursorY int

	queue        buttonQueue
	tickEvent    mouseEvent
	hasTickEvent bool
}

func NewInput(win *glfw.Window) *Input {
	in := &Input{win: win}
	win.SetMouseButtonCallback(in.onMouseButton)
	return in
}

func (in *Input) onMouseButton(w *glfw.Window, button glfw.MouseButton,
	action glfw.Action, _ glfw.ModifierKey) {
	if button != glfw.MouseButtonLeft {
		return
	}

	var pressed bool
	switch action {
	case glfw.Press:
		pressed = true
	case glfw.Release:
		pressed = false
	default:
		return
	}

	x, y := w.GetCursorPos()
	in.queue.push(mouseEvent{pressed: pressed, x: int(x), y: int(y)})
}

func (in *Input) PumpButtons() {
	in.prevLeft = in.currLeft
	in.hasTickEvent = false

	ev, ok := in.queue.pop()
	if !ok {
		return
	}

	in.currLeft = ev.pressed
	in.tickEvent = ev
	in.hasTickEvent = true
}

func (in *Input) SampleCursor() {
	x, y := in.win.GetCursorPos()
	in.cursorX, in.cursorY = int(x), int(y)
}

func (in *Input) LeftPressed() bool {
	return in.currLeft
}

func (in *Input) LeftPressEvent() (x, y int, ok bool) {
	if !in.hasTickEvent || !in.currLeft || in.prevLeft {
		return 0, 0, false
	}
	return in.tickEvent.x, in.tickEvent.y, true
}

func (in *Input) LeftReleaseEvent() (x, y int, ok bool) {
	if !in.hasTickEvent || in.currLeft || !in.prevLeft {
		return 0, 0, false
	}
	return in.tickEvent.x, in.tickEvent.y, true
}
