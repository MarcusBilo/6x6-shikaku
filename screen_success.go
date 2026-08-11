package main

// screen_success.go

func (g *Game) updateSuccess() {
	in := g.input
	if _, _, ok := in.LeftPressEvent(); ok {
		g.startNewPuzzle()
	}
}

func (g *Game) drawSuccessOverlay() {
	gfx := g.gfx
	font := g.font

	gfx.FillRect(0, 0, WindowSize, WindowSize, ColorWhite)

	posX := WindowSize / 2
	posY1 := WindowSize / 2
	posY2 := posY1 + int(float32(font.face.Height)*font.scale)*3/2
	font.DrawTextCentered(gfx, "Success!", posX, posY1, ColorBlack)
	font.DrawTextCentered(gfx, "Left click to play again", posX, posY2, ColorBlack)
}
