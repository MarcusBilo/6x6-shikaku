package main

// puzzle.go

import "math"

func init() {
	computeGeometryOnce()

	solverClues = make([]clueConstraint, maxClues)
	solverTrail = make([]trailEntry, 0, maxPlacementsPerShape)
	for i := range solverClues {
		solverClues[i].candidates = make([]gridRegion, 0, maxPlacementsPerClue)
		solverClues[i].live = make([]gridRegion, 0, maxPlacementsPerClue)
	}
}

// ---------------------------------------------------------------------------------------------------------------------

const (
	GridSize = 6
	maxClues = GridSize * GridSize
)

type gridRegion struct {
	rowMin, colMin, rowMax, colMax uint8
}

type clueCell struct {
	row, col, area int
}

var genParamCombos = []struct {
	depth           int
	mergeIterations int
	mergeProb       float64
}{
	{5, 1, 0.25},
	{5, 1, 0.5},
	{5, 1, 0.75},
	{5, 2, 0.25},
	{5, 2, 0.5},
	{5, 3, 0.25},
} // depth does not track GridSize; coincidental
// These settings yield a challenging-accept rate of ~10%
// fallback probability: (1-q)^N = 0.9^240 ~ 1.043×10^-11.

const genPuzzleAttempts = 240

var bestChallengingClues = make([]clueCell, maxClues)

var staticFallbackClues = []clueCell{
	{row: 0, col: 0, area: 9},
	{row: 0, col: 3, area: 2},
	{row: 1, col: 3, area: 4},
	{row: 1, col: 4, area: 3},
	{row: 1, col: 5, area: 3},
	{row: 3, col: 0, area: 3},
	{row: 3, col: 1, area: 2},
	{row: 3, col: 5, area: 3},
	{row: 4, col: 4, area: 1},
	{row: 5, col: 1, area: 4},
	{row: 5, col: 3, area: 2},
}

var staticFallbackGrid = func() [GridSize][GridSize]int {
	var grid [GridSize][GridSize]int
	for _, c := range staticFallbackClues {
		grid[c.row][c.col] = c.area
	}
	return grid
}()

func newPuzzle() ([GridSize][GridSize]int, []clueCell) {
	targetDensity := triangularTargetDensity()
	var bestChallengingGrid [GridSize][GridSize]int
	bestChallengingScore := math.Inf(-1)
	bestChallengingLen := 0
	foundChallenging := false

	for attempt := 0; attempt < genPuzzleAttempts; attempt++ {
		params := genParamCombos[randInt(len(genParamCombos))]
		candidateGrid, clues := generateGrid(params.depth, params.mergeIterations, params.mergeProb)

		score, branchNodes := scoreGrid(candidateGrid, clues, targetDensity)
		if score <= scoreRejected {
			continue
		}

		if branchNodes >= 1 && score > bestChallengingScore {
			bestChallengingScore = score
			bestChallengingGrid = candidateGrid
			bestChallengingLen = len(clues)
			copy(bestChallengingClues, clues)
			foundChallenging = true
		}
	}
	if foundChallenging {
		return bestChallengingGrid, bestChallengingClues[:bestChallengingLen]
	}
	return staticFallbackGrid, staticFallbackClues
}

func triangularTargetDensity() float64 {
	const (
		minClueCount = 9
		maxClueCount = 14
		minDensity   = float64(minClueCount) / (GridSize * GridSize)
		maxDensity   = float64(maxClueCount) / (GridSize * GridSize)
		densityRange = maxDensity - minDensity
	)
	sampleA := minDensity + randFloat64()*densityRange
	sampleB := minDensity + randFloat64()*densityRange
	return (sampleA + sampleB) / 2
}

// ---------------------------------------------------------------------------------------------------------------------

var (
	regionScratch       = make([]gridRegion, 0, maxClues)
	removedScratch      = make([]bool, 0, maxClues)
	pendingMergeScratch = make([]gridRegion, 0, maxClues)
	cluesScratch        = make([]clueCell, 0, maxClues)
)

func generateGrid(depth, mergeIterations int, mergeProb float64) ([GridSize][GridSize]int, []clueCell) {
	var grid [GridSize][GridSize]int

	regionScratch = regionScratch[:0]
	regions := recursivelySplitRegion(gridRegion{0, 0, GridSize - 1, GridSize - 1}, depth, regionScratch)

	for round := 0; round < mergeIterations; round++ {
		regionCount := len(regions)

		if cap(removedScratch) < regionCount {
			removedScratch = make([]bool, regionCount)
		} else {
			removedScratch = removedScratch[:regionCount]
			clear(removedScratch)
		}

		removedFlags := removedScratch[:regionCount] // Bounds Check Elimination
		pendingMerges := pendingMergeScratch[:0]

		for a := 0; a < regionCount; a++ {
			if removedFlags[a] {
				continue
			}
			regionA := regions[a]
			for b := a + 1; b < regionCount; b++ {
				if removedFlags[b] {
					continue
				}
				regionB := regions[b]
				if horizontallyAdjacent(regionA, regionB) {
					if randFloat64() < mergeProb {
						removedFlags[a] = true
						removedFlags[b] = true
						pendingMerges = append(pendingMerges, mergeHorizontally(regionA, regionB))
						break
					}
				} else if verticallyAdjacent(regionA, regionB) {
					if randFloat64() < mergeProb {
						removedFlags[a] = true
						removedFlags[b] = true
						pendingMerges = append(pendingMerges, mergeVertically(regionA, regionB))
						break
					}
				}
			}
		}
		pendingMergeScratch = pendingMerges

		writeIdx := 0
		for readIdx := 0; readIdx < regionCount; readIdx++ {
			if !removedFlags[readIdx] {
				regions[writeIdx] = regions[readIdx]
				writeIdx++
			}
		}
		survivors := regions[:writeIdx]

		// safe because writeIdx <= readIdx
		regions = append(survivors, pendingMerges...)
	}
	regionScratch = regions

	clues := cluesScratch[:0]
	for _, region := range regions {
		height := int(region.rowMax-region.rowMin) + 1
		width := int(region.colMax-region.colMin) + 1
		area := height * width
		row := int(region.rowMin) + randInt(height)
		col := int(region.colMin) + randInt(width)
		grid[row][col] = area
		clues = append(clues, clueCell{row: row, col: col, area: area})
	}
	cluesScratch = clues
	return grid, clues
}

func recursivelySplitRegion(region gridRegion, depth int, out []gridRegion) []gridRegion {
	rowMin, colMin, rowMax, colMax := region.rowMin, region.colMin, region.rowMax, region.colMax
	height := rowMax - rowMin + 1
	width := colMax - colMin + 1

	if depth == 0 || height*width <= 3 {
		return append(out, region)
	}

	var splitByRow bool
	switch {
	case rowMin == rowMax:
		splitByRow = false
	case colMin == colMax:
		splitByRow = true
	default:
		// splitByRow = randFloat64() < float64(height)/float64(height+width)
		splitByRow = float64(height+width)*randFloat64() < float64(height)
	}

	if splitByRow {
		splitRow := rowMin + uint8(randInt(int(rowMax-rowMin)))
		out = recursivelySplitRegion(gridRegion{rowMin, colMin, splitRow, colMax}, depth-1, out)
		out = recursivelySplitRegion(gridRegion{splitRow + 1, colMin, rowMax, colMax}, depth-1, out)
		return out
	}

	splitCol := colMin + uint8(randInt(int(colMax-colMin)))
	out = recursivelySplitRegion(gridRegion{rowMin, colMin, rowMax, splitCol}, depth-1, out)
	out = recursivelySplitRegion(gridRegion{rowMin, splitCol + 1, rowMax, colMax}, depth-1, out)
	return out
}

func horizontallyAdjacent(a, b gridRegion) bool {
	return a.rowMin == b.rowMin && a.rowMax == b.rowMax &&
		(a.colMax+1 == b.colMin || b.colMax+1 == a.colMin)
}

func verticallyAdjacent(a, b gridRegion) bool {
	return a.colMin == b.colMin && a.colMax == b.colMax &&
		(a.rowMax+1 == b.rowMin || b.rowMax+1 == a.rowMin)
}

func mergeHorizontally(a, b gridRegion) gridRegion {
	return gridRegion{a.rowMin, min(a.colMin, b.colMin), a.rowMax, max(a.colMax, b.colMax)}
}

func mergeVertically(a, b gridRegion) gridRegion {
	return gridRegion{min(a.rowMin, b.rowMin), a.colMin, max(a.rowMax, b.rowMax), a.colMax}
}

// ---------------------------------------------------------------------------------------------------------------------

const scoreRejected = -1000.0

func scoreGrid(grid [GridSize][GridSize]int, clues []clueCell, targetDensity float64) (score float64, branchNodes int) {

	clueCount := len(clues)
	if clueCount <= 8 {
		return scoreRejected, 0
	}
	singleCellClues := 0
	for _, clue := range clues {
		if clue.area == 1 {
			singleCellClues++
		}
	}
	if singleCellClues > 1 {
		return scoreRejected, 0
	}

	solutionCount, branchNodes, obviousClueCount := solve(grid)
	if solutionCount != 1 {
		return scoreRejected, branchNodes
	}

	stats := analyzeClues(clues)
	diversityRatio := float64(stats.distinctAreaCount) / float64(stats.clueCount)
	normalizedAreaRange := float64(stats.maxArea-stats.minArea) / float64(GridSize*GridSize)
	densityScore := 1 - clamp01(math.Abs(stats.density-targetDensity)/0.12)

	excessEmptyLines := float64(max(0, stats.emptyRows-1) + max(0, stats.emptyCols-1))
	clusterPenalty := 1.5*excessEmptyLines + 2.5*float64(stats.emptyQuadrantCount) + 1.0*stats.quadrantImbalance

	difficultyScore := clamp01(float64(branchNodes) / 4.0)

	nonTrivialClueCount := stats.clueCount - stats.singleCellClues
	obviousClueFraction := 0.0
	if nonTrivialClueCount > 0 {
		obviousClueFraction = float64(obviousClueCount) / float64(nonTrivialClueCount)
	}

	score = 3.0*diversityRatio + normalizedAreaRange + 3.0*densityScore +
		9.0*difficultyScore - 3.0*stats.mostCommonAreaFraction - clusterPenalty -
		6.0*obviousClueFraction
	return score, branchNodes
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	} else if x > 1 {
		return 1
	} else {
		return x
	}
}

type puzzleStats struct {
	clueCount              int
	distinctAreaCount      int
	minArea                int
	maxArea                int
	density                float64
	mostCommonAreaFraction float64
	emptyRows              int
	emptyCols              int
	emptyQuadrantCount     int
	quadrantImbalance      float64
	singleCellClues        int
}

func analyzeClues(clues []clueCell) puzzleStats {
	var areaCounts [GridSize*GridSize + 1]int
	minArea, maxArea := math.MaxInt32, 0
	distinctAreas, mostCommonAreaCount, singleCellClues := 0, 0, 0

	var quadrantCounts [4]int
	var rowHasClue [GridSize]bool
	var colHasClue [GridSize]bool

	for _, clue := range clues {
		area := clue.area
		areaCounts[area]++
		if areaCounts[area] == 1 {
			distinctAreas++
		}
		if areaCounts[area] > mostCommonAreaCount {
			mostCommonAreaCount = areaCounts[area]
		}
		if area < minArea {
			minArea = area
		}
		if area > maxArea {
			maxArea = area
		}
		if area == 1 {
			singleCellClues++
		}
		rowHasClue[clue.row] = true
		colHasClue[clue.col] = true
		quadrantCounts[quadrantByCell[clue.row][clue.col]]++
	}

	clueCount := len(clues)

	emptyRows, emptyCols := 0, 0
	for i := 0; i < GridSize; i++ {
		if !rowHasClue[i] {
			emptyRows++
		}
		if !colHasClue[i] {
			emptyCols++
		}
	}

	emptyQuadrantCount := 0
	quadrantCountMean := float64(clueCount) / 4
	quadrantCountVariance := 0.0
	for _, count := range quadrantCounts {
		if count == 0 {
			emptyQuadrantCount++
		}
		deviation := float64(count) - quadrantCountMean
		quadrantCountVariance += deviation * deviation
	}
	quadrantCountVariance /= 4
	quadrantImbalance := 0.0
	if quadrantCountMean > 0 {
		quadrantImbalance = math.Sqrt(quadrantCountVariance) / quadrantCountMean
	}

	mostCommonAreaFraction := 0.0
	if clueCount > 0 {
		mostCommonAreaFraction = float64(mostCommonAreaCount) / float64(clueCount)
	}

	return puzzleStats{
		clueCount:              clueCount,
		distinctAreaCount:      distinctAreas,
		minArea:                minArea,
		maxArea:                maxArea,
		density:                float64(clueCount) / float64(GridSize*GridSize),
		mostCommonAreaFraction: mostCommonAreaFraction,
		emptyRows:              emptyRows,
		emptyCols:              emptyCols,
		emptyQuadrantCount:     emptyQuadrantCount,
		quadrantImbalance:      quadrantImbalance,
		singleCellClues:        singleCellClues,
	}
}

const (
	maxPlacementsPerClue  = 22 // max rectangles for one cell and area; attained at area 12 on a 6x6 grid
	maxPlacementsPerShape = 90 // conservative upper bouhnd on candidate removal/assignments for a valid 6x6
)

const (
	trailCandidateRemoved = 0
	trailForcedAssignment = 1
)

type trailEntry struct {
	actionType uint8
	clueIdx    uint8
	restoreLen uint8
	region     gridRegion
}

type clueConstraint struct {
	area       int
	candidates []gridRegion
	live       []gridRegion // never re-slice live from a start other than index 0.
	assigned   bool
}

var (
	solverClues         []clueConstraint
	solverCellOccupied  [GridSize][GridSize]bool
	solverTrail         []trailEntry
	solverSolutionCount int
	solverBranchNodes   int
)

func solve(grid [GridSize][GridSize]int) (solutionCount, branchNodes, obviousClueCount int) {
	if collectClues(grid) == 0 {
		// Not an optimisation: With nothing to place, backtrack finds
		// every constraint satisfied and reports the empty grid as uniquely solvable.
		return 0, 0, 0
	}

	for i := range solverClues {
		clue := &solverClues[i]
		if clue.area != 1 && len(clue.candidates) == 1 {
			obviousClueCount++
		}
	}

	resetSolveState()
	backtrack()

	return solverSolutionCount, solverBranchNodes, obviousClueCount
}

func resetSolveState() {
	solverCellOccupied = [GridSize][GridSize]bool{}
	solverSolutionCount = 0
	solverBranchNodes = 0
	solverTrail = solverTrail[:0]

	for i := range solverClues {
		clue := &solverClues[i]
		clue.assigned = false
		clue.live = append(clue.live[:0], clue.candidates...)
	}
}

func assignClue(clueIdx int, region gridRegion) {
	solverClues[clueIdx].assigned = true
	for row := region.rowMin; row <= region.rowMax; row++ {
		for col := region.colMin; col <= region.colMax; col++ {
			solverCellOccupied[row][col] = true
		}
	}
}

func unassignClue(clueIdx int, region gridRegion) {
	solverClues[clueIdx].assigned = false
	for row := region.rowMin; row <= region.rowMax; row++ {
		for col := region.colMin; col <= region.colMax; col++ {
			solverCellOccupied[row][col] = false
		}
	}
}

func regionAvailable(region gridRegion) bool {
	for row := region.rowMin; row <= region.rowMax && row < GridSize; row++ { // Bounds Check Elimination
		rowCells := &solverCellOccupied[row]                                      // solverCellOccupied[row] is invariant across the inner loop; hoist it
		for col := region.colMin; col <= region.colMax && col < GridSize; col++ { // Bounds Check Elimination
			if rowCells[col] {
				return false
			}
		}
	}
	return true
}

// Swapped to the end, not overwritten, so undoTo can restore it when live grows.
func dropCandidate(clueIdx, pos int) {
	live := solverClues[clueIdx].live
	lastIdx := len(live) - 1
	live[pos], live[lastIdx] = live[lastIdx], live[pos]
	solverTrail = append(solverTrail, trailEntry{actionType: trailCandidateRemoved, clueIdx: uint8(clueIdx), restoreLen: uint8(len(live))})
	solverClues[clueIdx].live = live[:lastIdx]
}

func undoTo(trailMark int) {
	for len(solverTrail) > trailMark {
		entry := solverTrail[len(solverTrail)-1]
		solverTrail = solverTrail[:len(solverTrail)-1]
		switch entry.actionType {
		case trailCandidateRemoved:
			// given live is never re-sliced from a start other than 0, cap is always maxPlacementsPerClue.
			clue := &solverClues[entry.clueIdx]
			clue.live = clue.live[:entry.restoreLen]
		case trailForcedAssignment:
			unassignClue(int(entry.clueIdx), entry.region)
		}
	}
}

func propagate() bool {
	for {
		changed := false

		for clueIdx := range solverClues {
			// dropCandidate shortens live, so a hoisted copy would go stale.
			clue := &solverClues[clueIdx]
			if clue.assigned {
				continue
			}

			pos := 0
			for pos < len(clue.live) {
				if regionAvailable(clue.live[pos]) {
					pos++
				} else {
					dropCandidate(clueIdx, pos)
				}
			}

			switch len(clue.live) {
			case 0:
				return false
			case 1:
				region := clue.live[0]
				assignClue(clueIdx, region)
				solverTrail = append(solverTrail, trailEntry{actionType: trailForcedAssignment, clueIdx: uint8(clueIdx), region: region})
				changed = true
			}
		}

		if !changed {
			return true
		}
	}
}

// backtrack returns whether the caller must keep searching, not whether it succeeded.
func backtrack() bool {
	if !propagate() {
		return true
	}

	branchClue, ok := clueWithFewestCandidates()
	if !ok {
		solverSolutionCount++
		return solverSolutionCount < 2
	}

	solverBranchNodes++

	trailMark := len(solverTrail)

	for _, region := range solverClues[branchClue].live {
		undoTo(trailMark)

		assignClue(branchClue, region)

		if !backtrack() {
			unassignClue(branchClue, region)
			return false
		}

		unassignClue(branchClue, region)
	}

	return true
}

// minimum remaining values "fail-first" heuristic.
func clueWithFewestCandidates() (idx int, ok bool) {
	for i := range solverClues {
		if solverClues[i].assigned {
			continue
		}
		if !ok || len(solverClues[i].live) < len(solverClues[idx].live) {
			idx, ok = i, true
		}
	}
	return idx, ok
}

func collectClues(grid [GridSize][GridSize]int) int {
	clueCount := 0
	for row := 0; row < GridSize; row++ {
		for col := 0; col < GridSize; col++ {
			if grid[row][col] != 0 {
				clueCount++
			}
		}
	}

	solverClues = solverClues[:clueCount]
	if clueCount == 0 {
		return 0
	}

	buildPrefixSum(&grid)

	filled := 0
	for row := 0; row < GridSize; row++ {
		for col := 0; col < GridSize; col++ {
			area := grid[row][col]
			if area == 0 {
				continue
			}
			clue := &solverClues[filled]
			filled++
			clue.area = area
			clue.candidates = collectCandidates(row, col, area, clue.candidates[:0])
		}
	}
	return filled
}

type prefixSum [GridSize + 1][GridSize + 1]int

var solverPrefixSum prefixSum

func buildPrefixSum(grid *[GridSize][GridSize]int) {
	solverPrefixSum = prefixSum{}
	for row := 0; row < GridSize; row++ {
		for col := 0; col < GridSize; col++ {
			v := 0
			if grid[row][col] != 0 {
				v = 1
			}
			solverPrefixSum[row+1][col+1] = v + solverPrefixSum[row][col+1] + solverPrefixSum[row+1][col] - solverPrefixSum[row][col]
		}
	}
}

func collectCandidates(row, col, area int, buf []gridRegion) []gridRegion {
	for _, shape := range shapesByCellArea[row][col][area] {
		if regionHasNoOtherClue(shape) {
			buf = append(buf, shape)
		}
	}
	return buf
}

func regionHasNoOtherClue(region gridRegion) bool {
	clueCount := solverPrefixSum[region.rowMax+1][region.colMax+1] - solverPrefixSum[region.rowMin][region.colMax+1] -
		solverPrefixSum[region.rowMax+1][region.colMin] + solverPrefixSum[region.rowMin][region.colMin]
	return clueCount <= 1
}

const (
	// 1 + 3 + 6 + 10 + 15 + 21 = 56; n(n+1)/2
	cumulativeCellRangeCount = (GridSize * (GridSize + 1) * (GridSize + 2)) / 6
	totalCellShapeCount      = cumulativeCellRangeCount * cumulativeCellRangeCount
)

var (
	quadrantByCell   [GridSize][GridSize]int
	allShapesStorage [totalCellShapeCount]gridRegion
	shapesByCellArea [GridSize][GridSize][GridSize*GridSize + 1][]gridRegion
)

func computeGeometryOnce() {
	half := GridSize / 2
	shapeCursor := 0
	for row := 0; row < GridSize; row++ {
		for col := 0; col < GridSize; col++ {
			quadrant := 0
			if row >= half {
				quadrant += 2
			}
			if col >= half {
				quadrant++
			}
			quadrantByCell[row][col] = quadrant
			for area := 1; area <= GridSize*GridSize; area++ {
				start := shapeCursor
				shapeCursor = fillShapes(allShapesStorage[:], shapeCursor, row, col, area)

				shapesByCellArea[row][col][area] = allShapesStorage[start:shapeCursor:shapeCursor]

				assert(shapeCursor-start <= maxPlacementsPerClue,
					"score_grid: more placements of one area on one cell than maxPlacementsPerClue")
			}
		}
	}

	assert(shapeCursor == len(allShapesStorage),
		"score_grid: shape table did not fill allShapesStorage exactly")
}

func fillShapes(dst []gridRegion, cursor, row, col, area int) int {
	for height := 1; height <= area && height <= GridSize; height++ {
		if area%height != 0 {
			continue
		}
		width := area / height
		if width > GridSize {
			continue
		}
		rowLow, rowHigh := max(0, row-height+1), min(row, GridSize-height)
		colLow, colHigh := max(0, col-width+1), min(col, GridSize-width)
		for rowStart := rowLow; rowStart <= rowHigh; rowStart++ {
			rowEnd := rowStart + height - 1
			for colStart := colLow; colStart <= colHigh; colStart++ {
				dst[cursor] = gridRegion{uint8(rowStart), uint8(colStart), uint8(rowEnd), uint8(colStart + width - 1)}
				cursor++
			}
		}
	}
	return cursor
}
