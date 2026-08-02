package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	stdDraw "image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
)

const (
	outFile             = `internal/tray/icon_data.go`
	trayPreviewFile     = `internal/tray/vpn_icon.png`
	brandIconSourceFile = `internal/tray/bridge_icon_source.png`
	icoFile             = `internal/tray/app.ico`
	cmdIconICOFile      = `cmd/winres/icon.ico`
	cmdIconPNGFile      = `cmd/winres/icon.png`
	cmdIconSmallPNGFile = `cmd/winres/icon16.png`
	installerICOFile    = `cmd/installer/winres/icon.ico`
	installerPNGFile    = `cmd/installer/winres/icon.png`
	installerSmallFile  = `cmd/installer/winres/icon16.png`
)

var (
	traySizes = []int{16, 20, 24, 32}
	appSizes  = []int{16, 24, 32, 48, 64, 128, 256}
)

type visualMode int

const (
	modeIdle visualMode = iota
	modeActive
	modeBusy
	modeError
)

func main() {
	activeFrames := iconFrames(modeActive, 6)
	busyFrames := iconFrames(modeBusy, 8)
	idleICO := trayIconICO(modeIdle, 0, 1)
	activeICO := activeFrames[0]
	busyICO := busyFrames[0]
	errorICO := trayIconICO(modeError, 0, 1)
	brandIcon, err := loadPNG(brandIconSourceFile)
	if err != nil {
		exitf("Failed to load brand icon %s: %v", brandIconSourceFile, err)
	}
	appICO := appIconICO(brandIcon)

	trayPreview := renderIcon(256, modeActive, 1, 6)
	if err := writePNG(trayPreviewFile, trayPreview); err != nil {
		exitf("Failed to write tray preview PNG: %v", err)
	}
	if err := writeFile(icoFile, appICO); err != nil {
		exitf("Failed to write app ICO: %v", err)
	}
	for _, path := range []string{cmdIconICOFile, installerICOFile} {
		if err := writeFile(path, appICO); err != nil {
			exitf("Failed to write %s: %v", path, err)
		}
	}
	sourceIcon := renderBrandIcon(brandIcon, 256)
	for _, path := range []string{cmdIconPNGFile, installerPNGFile} {
		if err := writePNG(path, sourceIcon); err != nil {
			exitf("Failed to write %s: %v", path, err)
		}
	}
	smallIcon := renderBrandIcon(brandIcon, 32)
	for _, path := range []string{cmdIconSmallPNGFile, installerSmallFile} {
		if err := writePNG(path, smallIcon); err != nil {
			exitf("Failed to write %s: %v", path, err)
		}
	}

	out, err := os.Create(outFile)
	if err != nil {
		exitf("Failed to create output file: %v", err)
	}
	defer out.Close()

	fmt.Fprintln(out, "package tray")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "// 自动生成的图标数据，请勿手动编辑")
	fmt.Fprintln(out, "// 托盘状态图标由 tools/icon_gen 生成；应用品牌图标通过 app_icon.go 嵌入。")
	fmt.Fprintln(out)
	writeVar(out, "iconIdle", idleICO)
	fmt.Fprintln(out)
	writeVar(out, "iconActive", activeICO)
	fmt.Fprintln(out)
	writeVar(out, "iconBusy", busyICO)
	fmt.Fprintln(out)
	writeVar(out, "iconError", errorICO)
	fmt.Fprintln(out)
	writeVarList(out, "iconActiveFrames", activeFrames)
	fmt.Fprintln(out)
	writeVarList(out, "iconBusyFrames", busyFrames)

	fmt.Println("icon_data.go generated successfully.")
}

func iconFrames(mode visualMode, count int) [][]byte {
	frames := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		frames = append(frames, trayIconICO(mode, i, count))
	}
	return frames
}

func trayIconICO(mode visualMode, frame, frameCount int) []byte {
	images := make([]sizedImage, 0, len(traySizes))
	for _, size := range traySizes {
		images = append(images, sizedImage{
			size: size,
			data: icoDIB(renderIcon(size, mode, frame, frameCount)),
		})
	}
	return toICO(images)
}

func appIconICO(source image.Image) []byte {
	images := make([]sizedImage, 0, len(appSizes))
	for _, size := range appSizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, renderBrandIcon(source, size)); err != nil {
			exitf("Failed to encode %dpx app icon: %v", size, err)
		}
		images = append(images, sizedImage{size: size, data: buf.Bytes()})
	}
	return toICO(images)
}

func renderIcon(size int, mode visualMode, frame, frameCount int) *image.RGBA {
	const supersample = 4
	canvasSize := size * supersample
	img := image.NewRGBA(image.Rect(0, 0, canvasSize, canvasSize))
	scale := float64(canvasSize) / 32.0
	drawIconSymbol(img, scale, mode, frame, frameCount)

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), stdDraw.Over, nil)
	return dst
}

func renderBrandIcon(source image.Image, size int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), source, source.Bounds(), stdDraw.Src, nil)
	applyRoundedAlpha(dst, float64(size)*0.13)
	return dst
}

func applyRoundedAlpha(img *image.RGBA, radius float64) {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			nx := math.Max(radius, math.Min(float64(x)+0.5, float64(width)-radius))
			ny := math.Max(radius, math.Min(float64(y)+0.5, float64(height)-radius))
			dx := float64(x) + 0.5 - nx
			dy := float64(y) + 0.5 - ny
			if dx*dx+dy*dy > radius*radius {
				offset := img.PixOffset(x, y)
				img.Pix[offset+3] = 0
			}
		}
	}
}

func drawIconSymbol(img *image.RGBA, s float64, mode visualMode, frame, frameCount int) {
	bg := color.RGBA{9, 32, 51, 255}
	routeMain := color.RGBA{248, 250, 252, 255}
	routeAccent := color.RGBA{56, 189, 248, 255}

	switch mode {
	case modeIdle:
		bg = color.RGBA{51, 65, 85, 255}
		routeMain = color.RGBA{226, 232, 240, 225}
		routeAccent = color.RGBA{148, 163, 184, 230}
	case modeError:
		bg = color.RGBA{51, 65, 85, 255}
		routeMain = color.RGBA{226, 232, 240, 215}
		routeAccent = color.RGBA{148, 163, 184, 225}
	}

	fillRoundedRect(img, 4*s, 4.6*s, 24*s, 24*s, 6.8*s, color.RGBA{2, 6, 23, 58})
	fillRoundedRect(img, 3*s, 3*s, 26*s, 26*s, 7*s, bg)
	fillRoundedRect(img, 4.3*s, 4.3*s, 23.4*s, 9*s, 5.2*s, color.RGBA{255, 255, 255, 22})

	top := point{16 * s, 7.2 * s}
	center := point{16 * s, 14.1 * s}
	left := point{9.4 * s, 21.3 * s}
	right := point{22.8 * s, 21.3 * s}
	stroke := 4.1 * s

	strokeLine(img, top, center, stroke, routeMain)
	strokeLine(img, center, left, stroke, routeMain)
	strokeLine(img, center, right, stroke, routeAccent)
	fillCircle(img, center.x, center.y, 2.15*s, color.RGBA{248, 250, 252, 255})

	switch mode {
	case modeActive:
		drawActivePulse(img, s, frame, frameCount)
		drawStatusBadge(img, s, color.RGBA{22, 163, 74, 255}, frame, frameCount)
	case modeBusy:
		drawBusySpinner(img, s, frame)
		drawStatusBadge(img, s, color.RGBA{245, 158, 11, 255}, 0, 1)
	case modeIdle:
		strokeLine(img, point{8.2 * s, 8.2 * s}, point{23.8 * s, 23.8 * s}, 4.3*s, color.RGBA{239, 68, 68, 238})
	case modeError:
		strokeLine(img, point{8.2 * s, 8.2 * s}, point{23.8 * s, 23.8 * s}, 4.5*s, color.RGBA{239, 68, 68, 255})
		drawStatusBadge(img, s, color.RGBA{239, 68, 68, 255}, 0, 1)
	}
}

func drawActivePulse(img *image.RGBA, s float64, frame, frameCount int) {
	segments := [][2]point{
		{{16 * s, 7.2 * s}, {16 * s, 14.1 * s}},
		{{16 * s, 14.1 * s}, {22.8 * s, 21.3 * s}},
		{{18.2 * s, 16.5 * s}, {22.8 * s, 21.3 * s}},
		{{16 * s, 14.1 * s}, {9.4 * s, 21.3 * s}},
		{{13.8 * s, 16.5 * s}, {9.4 * s, 21.3 * s}},
		{{16 * s, 7.2 * s}, {16 * s, 14.1 * s}},
	}
	seg := segments[frame%len(segments)]
	strokeLine(img, seg[0], seg[1], 5.1*s, color.RGBA{34, 197, 94, 215})
	strokeLine(img, seg[0], seg[1], 2.5*s, color.RGBA{236, 253, 245, 245})

	p := interpolate(seg[0], seg[1], 0.72)
	fillCircle(img, p.x, p.y, 4.1*s, color.RGBA{34, 197, 94, 44})
	fillCircle(img, p.x, p.y, 2.05*s, color.RGBA{34, 197, 94, 255})
}

func drawBusySpinner(img *image.RGBA, s float64, frame int) {
	center := point{16 * s, 16 * s}
	radius := 12.1 * s
	for i := 0; i < 4; i++ {
		alpha := uint8(95 + i*42)
		angle := (float64(frame+i) / 8.0) * 2 * math.Pi
		x := center.x + math.Cos(angle)*radius
		y := center.y + math.Sin(angle)*radius
		fillCircle(img, x, y, (1.55+float64(i)*0.16)*s, color.RGBA{245, 158, 11, alpha})
	}
}

func drawStatusBadge(img *image.RGBA, s float64, c color.RGBA, frame, frameCount int) {
	radius := 4.6 * s
	if frameCount > 1 {
		radius = (4.05 + 0.45*math.Sin(float64(frame)*2*math.Pi/float64(frameCount))) * s
	}
	x := 24.5 * s
	y := 24.5 * s
	fillCircle(img, x, y, 5.9*s, color.RGBA{255, 255, 255, 242})
	fillCircle(img, x, y, radius, c)
}

type point struct {
	x float64
	y float64
}

func interpolate(a, b point, t float64) point {
	return point{
		x: a.x + (b.x-a.x)*t,
		y: a.y + (b.y-a.y)*t,
	}
}

func fillRoundedRect(img *image.RGBA, x, y, w, h, r float64, c color.RGBA) {
	minX := maxInt(0, int(math.Floor(x)))
	minY := maxInt(0, int(math.Floor(y)))
	maxX := minInt(img.Bounds().Dx(), int(math.Ceil(x+w)))
	maxY := minInt(img.Bounds().Dy(), int(math.Ceil(y+h)))
	for py := minY; py < maxY; py++ {
		for px := minX; px < maxX; px++ {
			fx := float64(px) + 0.5
			fy := float64(py) + 0.5
			nx := math.Max(x+r, math.Min(fx, x+w-r))
			ny := math.Max(y+r, math.Min(fy, y+h-r))
			if (fx-nx)*(fx-nx)+(fy-ny)*(fy-ny) <= r*r {
				blendPixel(img, px, py, c)
			}
		}
	}
}

func strokeLine(img *image.RGBA, a, b point, width float64, c color.RGBA) {
	radius := width / 2
	minX := maxInt(0, int(math.Floor(math.Min(a.x, b.x)-radius)))
	minY := maxInt(0, int(math.Floor(math.Min(a.y, b.y)-radius)))
	maxX := minInt(img.Bounds().Dx(), int(math.Ceil(math.Max(a.x, b.x)+radius)))
	maxY := minInt(img.Bounds().Dy(), int(math.Ceil(math.Max(a.y, b.y)+radius)))
	for py := minY; py < maxY; py++ {
		for px := minX; px < maxX; px++ {
			if distanceToSegment(float64(px)+0.5, float64(py)+0.5, a, b) <= radius {
				blendPixel(img, px, py, c)
			}
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r float64, c color.RGBA) {
	minX := maxInt(0, int(math.Floor(cx-r)))
	minY := maxInt(0, int(math.Floor(cy-r)))
	maxX := minInt(img.Bounds().Dx(), int(math.Ceil(cx+r)))
	maxY := minInt(img.Bounds().Dy(), int(math.Ceil(cy+r)))
	r2 := r * r
	for py := minY; py < maxY; py++ {
		for px := minX; px < maxX; px++ {
			dx := float64(px) + 0.5 - cx
			dy := float64(py) + 0.5 - cy
			if dx*dx+dy*dy <= r2 {
				blendPixel(img, px, py, c)
			}
		}
	}
}

func distanceToSegment(px, py float64, a, b point) float64 {
	vx := b.x - a.x
	vy := b.y - a.y
	wx := px - a.x
	wy := py - a.y
	length2 := vx*vx + vy*vy
	if length2 == 0 {
		return math.Hypot(px-a.x, py-a.y)
	}
	t := math.Max(0, math.Min(1, (wx*vx+wy*vy)/length2))
	projX := a.x + t*vx
	projY := a.y + t*vy
	return math.Hypot(px-projX, py-projY)
}

func blendPixel(img *image.RGBA, x, y int, src color.RGBA) {
	if src.A == 0 {
		return
	}
	dst := img.RGBAAt(x, y)
	srcA := float64(src.A) / 255.0
	dstA := float64(dst.A) / 255.0
	outA := srcA + dstA*(1-srcA)
	if outA <= 0 {
		return
	}
	outR := (float64(src.R)*srcA + float64(dst.R)*dstA*(1-srcA)) / outA
	outG := (float64(src.G)*srcA + float64(dst.G)*dstA*(1-srcA)) / outA
	outB := (float64(src.B)*srcA + float64(dst.B)*dstA*(1-srcA)) / outA
	img.SetRGBA(x, y, color.RGBA{
		R: clamp(outR),
		G: clamp(outG),
		B: clamp(outB),
		A: clamp(outA * 255),
	})
}

type sizedImage struct {
	size int
	data []byte
}

func icoDIB(img *image.RGBA) []byte {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	xorSize := width * height * 4
	maskStride := ((width + 31) / 32) * 4
	maskSize := maskStride * height
	data := make([]byte, 40+xorSize+maskSize)

	binary.LittleEndian.PutUint32(data[0:4], 40)
	binary.LittleEndian.PutUint32(data[4:8], uint32(width))
	binary.LittleEndian.PutUint32(data[8:12], uint32(height*2))
	binary.LittleEndian.PutUint16(data[12:14], 1)
	binary.LittleEndian.PutUint16(data[14:16], 32)
	binary.LittleEndian.PutUint32(data[16:20], 0)
	binary.LittleEndian.PutUint32(data[20:24], uint32(xorSize+maskSize))

	pixOff := 40
	for y := height - 1; y >= 0; y-- {
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			data[pixOff] = byte(b >> 8)
			data[pixOff+1] = byte(g >> 8)
			data[pixOff+2] = byte(r >> 8)
			data[pixOff+3] = byte(a >> 8)
			pixOff += 4
		}
	}

	maskOff := 40 + xorSize
	for y := height - 1; y >= 0; y-- {
		for x := 0; x < width; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a>>8 >= 128 {
				continue
			}
			byteIndex := maskOff + (height-1-y)*maskStride + x/8
			data[byteIndex] |= 0x80 >> uint(x%8)
		}
	}

	return data
}

func toICO(images []sizedImage) []byte {
	headerSize := 6 + len(images)*16
	totalSize := headerSize
	for _, img := range images {
		totalSize += len(img.data)
	}

	ico := make([]byte, totalSize)
	binary.LittleEndian.PutUint16(ico[0:2], 0)
	binary.LittleEndian.PutUint16(ico[2:4], 1)
	binary.LittleEndian.PutUint16(ico[4:6], uint16(len(images)))

	offset := headerSize
	for i, img := range images {
		entry := 6 + i*16
		if img.size >= 256 {
			ico[entry] = 0
			ico[entry+1] = 0
		} else {
			ico[entry] = byte(img.size)
			ico[entry+1] = byte(img.size)
		}
		ico[entry+2] = 0
		ico[entry+3] = 0
		binary.LittleEndian.PutUint16(ico[entry+4:entry+6], 1)
		binary.LittleEndian.PutUint16(ico[entry+6:entry+8], 32)
		binary.LittleEndian.PutUint32(ico[entry+8:entry+12], uint32(len(img.data)))
		binary.LittleEndian.PutUint32(ico[entry+12:entry+16], uint32(offset))
		copy(ico[offset:], img.data)
		offset += len(img.data)
	}

	return ico
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func writeVar(f *os.File, name string, data []byte) {
	fmt.Fprintf(f, "var %s = []byte{", name)
	for i, b := range data {
		if i%16 == 0 {
			fmt.Fprintf(f, "\n\t")
		}
		fmt.Fprintf(f, "0x%02x,", b)
		if i%16 != 15 && i != len(data)-1 {
			fmt.Fprint(f, " ")
		}
	}
	fmt.Fprintln(f, "\n}")
}

func writeVarList(f *os.File, name string, list [][]byte) {
	fmt.Fprintf(f, "var %s = [][]byte{\n", name)
	for _, data := range list {
		fmt.Fprint(f, "\t{")
		for i, b := range data {
			if i%16 == 0 {
				fmt.Fprintf(f, "\n\t\t")
			}
			fmt.Fprintf(f, "0x%02x,", b)
			if i%16 != 15 && i != len(data)-1 {
				fmt.Fprint(f, " ")
			}
		}
		fmt.Fprintln(f, "\n\t},")
	}
	fmt.Fprintln(f, "}")
}

func clamp(v float64) uint8 {
	return uint8(math.Max(0, math.Min(255, math.Round(v))))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
