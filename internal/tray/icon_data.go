package tray

// Minimal 16x16 ICO format icons (single color)

var iconIdle = generateIcon(128, 128, 128) // gray
var iconActive = generateIcon(0, 200, 0)   // green
var iconBusy = generateIcon(255, 200, 0)   // yellow
var iconError = generateIcon(200, 0, 0)    // red

func generateIcon(r, g, b byte) []byte {
	// ICO header (6 bytes) + directory entry (16 bytes) + BMP header + pixels
	width, height := 16, 16
	bmpSize := width * height * 4
	dataOffset := 6 + 16 + 40 // ICO header + dir entry + BITMAPINFOHEADER

	ico := make([]byte, dataOffset+bmpSize)

	// ICO Header
	ico[0] = 0 // reserved
	ico[1] = 0
	ico[2] = 1 // type: icon
	ico[3] = 0
	ico[4] = 1 // count: 1
	ico[5] = 0

	// Directory entry
	ico[6] = byte(width)  // width
	ico[7] = byte(height) // height
	ico[8] = 0            // color palette
	ico[9] = 0            // reserved
	ico[10] = 1           // color planes
	ico[11] = 0
	ico[12] = 32 // bits per pixel
	ico[13] = 0
	// size of image data
	size := uint32(40 + bmpSize)
	ico[14] = byte(size)
	ico[15] = byte(size >> 8)
	ico[16] = byte(size >> 16)
	ico[17] = byte(size >> 24)
	// offset
	ico[18] = byte(6 + 16)
	ico[19] = 0
	ico[20] = 0
	ico[21] = 0

	// BITMAPINFOHEADER
	off := 22
	ico[off] = 40 // header size
	ico[off+4] = byte(width)
	ico[off+8] = byte(height * 2) // height is doubled in ICO
	ico[off+12] = 1               // planes
	ico[off+14] = 32              // bpp

	// Pixel data (BGRA, bottom-up)
	off = dataOffset
	for i := 0; i < width*height; i++ {
		ico[off+i*4] = b     // blue
		ico[off+i*4+1] = g   // green
		ico[off+i*4+2] = r   // red
		ico[off+i*4+3] = 255 // alpha
	}

	return ico
}
