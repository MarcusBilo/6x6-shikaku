## One Grid, Endless Puzzles

This is a simple, endlessly playable Shikaku puzzle game using a 6×6 grid. Each puzzle is generated automatically as you play, giving you an endless stream of new puzzles.


## Complete a puzzle

- Click and drag to draw rectangles on the grid.
- Each rectangle must contain exactly one clue.
- The rectangle’s area must equal the number shown by its clue.
- Rectangles must cover the entire grid exactly once.


## How to Play

### Option 1: Download Pre-built (Windows x86-64)
1. Go to the [Releases](../../releases) page
2. Download the latest `.zip` file
3. Extract the contents to a folder of your choice
4. Run the `.exe` file

### Option 2: Build from Source
1. Make sure you have [Go](https://golang.org/dl/) installed (version ≥ 1.25.0 at best)
2. Clone this repository:
   ```bash
   git clone https://github.com/MarcusBilo/blackjack-tui
   cd blackjack-tui
   ```
3. Build and run:
   ```bash
   go build
   .\blackjack-tui
   ```


## Controls

| Key | Action |
|---|---|
| **Mouse** | Draw rectangles |
| **Esc** | Open the settings menu |
| **F3** | Toggle debug information |

From the settings menu, you can configure the game's maximum FPS and TPS.


## Credits

- [go-gl/gl](https://github.com/go-gl/gl)
- [go-gl/glfw](https://github.com/go-gl/glfw)
- [go-gl/mathgl](https://github.com/go-gl/mathgl)

### Honorable Mention

Before switching to OpenGL, I used ebiten as a draft framework to prototype the game.

@hajimehoshi [2D game engine](hajimehoshi/ebiten)
