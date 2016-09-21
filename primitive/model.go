package primitive

import (
	"fmt"
	"image"
	"math/rand"
	"strings"

	"github.com/fogleman/gg"
)

type Model struct {
	W, H       int
	Background Color
	Target     *image.RGBA
	Current    *image.RGBA
	Buffer     *image.RGBA
	Context    *gg.Context
	Score      float64
	Size       int
	Mode       Mode
	Shapes     []Shape
	Alphas     []int
	Scores     []float64
	SVGs       []string
}

func NewModel(target image.Image, background Color, size int, mode Mode) *Model {
	model := &Model{}
	model.W = target.Bounds().Size().X
	model.H = target.Bounds().Size().Y
	model.Background = background
	model.Size = size
	model.Mode = mode
	model.Target = imageToRGBA(target)
	model.Current = uniformRGBA(target.Bounds(), background.NRGBA())
	model.Buffer = uniformRGBA(target.Bounds(), background.NRGBA())
	model.Score = differenceFull(model.Target, model.Current)
	model.Context = model.newContext()
	return model
}

func (model *Model) sizeAndScale() (w, h int, scale float64) {
	aspect := float64(model.W) / float64(model.H)
	if aspect >= 1 {
		w = model.Size
		h = int(float64(model.Size) / aspect)
		scale = float64(model.Size) / float64(model.W)
	} else {
		w = int(float64(model.Size) * aspect)
		h = model.Size
		scale = float64(model.Size) / float64(model.H)
	}
	return
}

func (model *Model) newContext() *gg.Context {
	w, h, scale := model.sizeAndScale()
	dc := gg.NewContext(w, h)
	dc.Scale(scale, scale)
	dc.Translate(0.5, 0.5)
	dc.SetColor(model.Background.NRGBA())
	dc.Clear()
	return dc
}

func (model *Model) Frames(scoreDelta float64) []image.Image {
	var result []image.Image
	dc := model.newContext()
	result = append(result, imageToRGBA(dc.Image()))
	previous := 10.0
	for i, shape := range model.Shapes {
		c := model.computeColor(shape.Rasterize(), model.Alphas[i])
		dc.SetRGBA255(c.R, c.G, c.B, c.A)
		shape.Draw(dc)
		dc.Fill()
		score := model.Scores[i]
		delta := previous - score
		if delta >= scoreDelta {
			previous = score
			result = append(result, imageToRGBA(dc.Image()))
		}
	}
	return result
}

func (model *Model) SVG() string {
	w, h, scale := model.sizeAndScale()
	c := model.Background
	var lines []string
	lines = append(lines, fmt.Sprintf("<svg xmlns=\"http://www.w3.org/2000/svg\" version=\"1.1\" width=\"%d\" height=\"%d\">", w, h))
	lines = append(lines, fmt.Sprintf("<rect x=\"0\" y=\"0\" width=\"%d\" height=\"%d\" fill=\"#%02x%02x%02x\" />", w, h, c.R, c.G, c.B))
	lines = append(lines, fmt.Sprintf("<g transform=\"scale(%f) translate(0.5 0.5)\">", scale))
	lines = append(lines, model.SVGs...)
	lines = append(lines, "</g>")
	lines = append(lines, "</svg>")
	return strings.Join(lines, "\n")
}

func (model *Model) Add(shape Shape, alpha int) {
	lines := shape.Rasterize()
	c := model.computeColor(lines, alpha)
	s := model.computeScore(lines, c, model.Buffer)
	Draw(model.Current, c, lines)

	attrs := "fill=\"#%02x%02x%02x\" fill-opacity=\"%f\""
	attrs = fmt.Sprintf(attrs, c.R, c.G, c.B, float64(c.A)/255)
	svg := shape.SVG(attrs)

	model.Score = s
	model.Shapes = append(model.Shapes, shape)
	model.Alphas = append(model.Alphas, alpha)
	model.Scores = append(model.Scores, s)
	model.SVGs = append(model.SVGs, svg)

	model.Context.SetRGBA255(c.R, c.G, c.B, c.A)
	shape.Draw(model.Context)
	model.Context.Fill()
}

func (model *Model) Step() {
	state := model.BestHillClimbState(model.Buffer, model.Mode, 100, 100, 10)
	// state := model.BestRandomState(model.Buffer, model.Mode, 1000)
	// state = Anneal(state, 0.1, 0.00001, 25000).(*State)
	state = HillClimb(state, 1000).(*State)
	model.Add(state.Shape, state.Alpha)
}

func (model *Model) BestHillClimbState(buffer *image.RGBA, t Mode, n, age, m int) *State {
	var bestEnergy float64
	var bestState *State
	for i := 0; i < m; i++ {
		state := model.BestRandomState(buffer, t, n)
		before := state.Energy()
		state = HillClimb(state, age).(*State)
		energy := state.Energy()
		vv("%dx random: %.6f -> %dx hill climb: %.6f\n", n, before, age, energy)
		if i == 0 || energy < bestEnergy {
			bestEnergy = energy
			bestState = state
		}
	}
	return bestState
}

func (model *Model) BestRandomState(buffer *image.RGBA, t Mode, n int) *State {
	var bestEnergy float64
	var bestState *State
	for i := 0; i < n; i++ {
		state := model.RandomState(buffer, t)
		energy := state.Energy()
		if i == 0 || energy < bestEnergy {
			bestEnergy = energy
			bestState = state
		}
	}
	return bestState
}

func (model *Model) RandomState(buffer *image.RGBA, t Mode) *State {
	switch t {
	default:
		return model.RandomState(buffer, Mode(rand.Intn(5)+1))
	case ModeTriangle:
		return NewState(model, buffer, NewRandomTriangle(model.W, model.H))
	case ModeRectangle:
		return NewState(model, buffer, NewRandomRectangle(model.W, model.H))
	case ModeEllipse:
		return NewState(model, buffer, NewRandomEllipse(model.W, model.H))
	case ModeCircle:
		return NewState(model, buffer, NewRandomCircle(model.W, model.H))
	case ModeRotatedRectangle:
		return NewState(model, buffer, NewRandomRotatedRectangle(model.W, model.H))
	}
}

func (model *Model) computeColor(lines []Scanline, alpha int) Color {
	var count int
	var rsum, gsum, bsum float64
	a := float64(alpha) / 255
	for _, line := range lines {
		i := model.Target.PixOffset(line.X1, line.Y)
		for x := line.X1; x <= line.X2; x++ {
			count++
			tr := float64(model.Target.Pix[i])
			tg := float64(model.Target.Pix[i+1])
			tb := float64(model.Target.Pix[i+2])
			cr := float64(model.Current.Pix[i])
			cg := float64(model.Current.Pix[i+1])
			cb := float64(model.Current.Pix[i+2])
			i += 4
			rsum += (a*cr - cr + tr) / a
			gsum += (a*cg - cg + tg) / a
			bsum += (a*cb - cb + tb) / a
		}
	}
	if count == 0 {
		return Color{}
	}
	r := clampInt(int(rsum/float64(count)), 0, 255)
	g := clampInt(int(gsum/float64(count)), 0, 255)
	b := clampInt(int(bsum/float64(count)), 0, 255)
	return Color{r, g, b, alpha}
}

func (model *Model) computeScore(lines []Scanline, c Color, buffer *image.RGBA) float64 {
	copy(buffer.Pix, model.Current.Pix)
	Draw(buffer, c, lines)
	return differencePartial(model.Target, model.Current, buffer, model.Score, lines)
}

func (model *Model) Energy(shape Shape, alpha int, buffer *image.RGBA) float64 {
	lines := shape.Rasterize()
	c := model.computeColor(lines, alpha)
	s := model.computeScore(lines, c, buffer)
	return s
}
