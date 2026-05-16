package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"

	"golang.org/x/image/draw"
)

const (
	iconSize = 32
	outFile  = `internal/tray/icon_data.go`
	srcFile  = `internal/tray/vpn_icon.png`
	icoFile  = `internal/tray/app.ico`
)

func main() {
	// Read PNG
	f, err := os.Open(srcFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open PNG: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	srcImg, err := png.Decode(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to decode PNG: %v\n", err)
		os.Exit(1)
	}

	// Scale to 32x32 using high-quality CatmullRom
	dst := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

	// Generate 4 variants
	idleImg := applyGrayscale(dst)
	activeImg := dst // keep original
	busyImg := applyTint(dst, color.RGBA{255, 200, 0, 80})
	errorImg := applyTint(dst, color.RGBA{200, 0, 0, 100})

	// Convert to ICO bytes
	idleICO := toICO(idleImg)
	activeICO := toICO(activeImg)
	busyICO := toICO(busyImg)
	errorICO := toICO(errorImg)
	appICO := toMultiSizeICO(srcImg, []int{16, 24, 32, 48, 64, 128, 256})

	if err := os.WriteFile(icoFile, appICO, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write app ICO: %v\n", err)
		os.Exit(1)
	}

	// Write Go source
	out, err := os.Create(outFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output file: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()

	fmt.Fprintln(out, "package tray")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "// 自动生成的图标数据，请勿手动编辑")
	fmt.Fprintln(out, "// 源图片: vpn_icon.png")
	fmt.Fprintln(out)
	writeVar(out, "iconIdle", idleICO)
	fmt.Fprintln(out)
	writeVar(out, "iconActive", activeICO)
	fmt.Fprintln(out)
	writeVar(out, "iconBusy", busyICO)
	fmt.Fprintln(out)
	writeVar(out, "iconError", errorICO)
	fmt.Fprintln(out)
	writeVar(out, "AppIcon", appICO)

	fmt.Println("icon_data.go generated successfully.")
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

func applyGrayscale(src *image.RGBA) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			// Luminance formula
			gray := uint8((0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 256.0)
			dst.SetRGBA(x, y, color.RGBA{gray, gray, gray, uint8(a >> 8)})
		}
	}
	return dst
}

func applyTint(src *image.RGBA, tint color.RGBA) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	tintAlpha := float64(tint.A) / 255.0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			sr := float64(r >> 8)
			sg := float64(g >> 8)
			sb := float64(b >> 8)

			// Blend with tint color
			nr := sr*(1-tintAlpha) + float64(tint.R)*tintAlpha
			ng := sg*(1-tintAlpha) + float64(tint.G)*tintAlpha
			nb := sb*(1-tintAlpha) + float64(tint.B)*tintAlpha

			dst.SetRGBA(x, y, color.RGBA{
				clamp(nr), clamp(ng), clamp(nb), uint8(a >> 8),
			})
		}
	}
	return dst
}

func clamp(v float64) uint8 {
	return uint8(math.Max(0, math.Min(255, v)))
}

func toICO(img *image.RGBA) []byte {
	width := iconSize
	height := iconSize
	bmpSize := width * height * 4
	headerSize := 6 + 16 + 40 // ICO header + dir entry + BITMAPINFOHEADER

	ico := make([]byte, headerSize+bmpSize)

	// ICO Header (6 bytes)
	binary.LittleEndian.PutUint16(ico[0:2], 0) // reserved
	binary.LittleEndian.PutUint16(ico[2:4], 1) // type: icon
	binary.LittleEndian.PutUint16(ico[4:6], 1) // count: 1 image

	// Directory entry (16 bytes starting at offset 6)
	ico[6] = byte(width)                                          // width (0 means 256)
	ico[7] = byte(height)                                         // height
	ico[8] = 0                                                    // color palette count
	ico[9] = 0                                                    // reserved
	binary.LittleEndian.PutUint16(ico[10:12], 1)                  // color planes
	binary.LittleEndian.PutUint16(ico[12:14], 32)                 // bits per pixel
	binary.LittleEndian.PutUint32(ico[14:18], uint32(40+bmpSize)) // image data size
	binary.LittleEndian.PutUint32(ico[18:22], 22)                 // offset to image data (6+16=22)

	// BITMAPINFOHEADER (40 bytes starting at offset 22)
	bmpOff := 22
	binary.LittleEndian.PutUint32(ico[bmpOff:bmpOff+4], 40)                  // header size
	binary.LittleEndian.PutUint32(ico[bmpOff+4:bmpOff+8], uint32(width))     // width
	binary.LittleEndian.PutUint32(ico[bmpOff+8:bmpOff+12], uint32(height*2)) // height (doubled for ICO)
	binary.LittleEndian.PutUint16(ico[bmpOff+12:bmpOff+14], 1)               // planes
	binary.LittleEndian.PutUint16(ico[bmpOff+14:bmpOff+16], 32)              // bpp
	binary.LittleEndian.PutUint32(ico[bmpOff+16:bmpOff+20], 0)               // compression
	binary.LittleEndian.PutUint32(ico[bmpOff+20:bmpOff+24], uint32(bmpSize)) // image size
	// remaining fields (ppm x/y, colors used/important) stay 0

	// Pixel data (BGRA, bottom-to-top row order)
	pixOff := headerSize
	for y := height - 1; y >= 0; y-- {
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			ico[pixOff] = byte(b >> 8)   // Blue
			ico[pixOff+1] = byte(g >> 8) // Green
			ico[pixOff+2] = byte(r >> 8) // Red
			ico[pixOff+3] = byte(a >> 8) // Alpha
			pixOff += 4
		}
	}

	return ico
}

func toMultiSizeICO(src image.Image, sizes []int) []byte {
	type imageData struct {
		size int
		data []byte
	}

	images := make([]imageData, 0, len(sizes))
	for _, size := range sizes {
		dst := image.NewRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to encode %dpx icon: %v\n", size, err)
			os.Exit(1)
		}
		images = append(images, imageData{size: size, data: buf.Bytes()})
	}

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
