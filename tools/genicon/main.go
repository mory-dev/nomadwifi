// Command genicon builds assets/nomadwifi.ico from the brand logo.
//
// The icon is generated at build time rather than checked in as a binary blob
// so every size stays in step with logo.png, and so the build needs no image
// tooling installed. Run it with: go run ./tools/genicon
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// sizes covers everything Windows asks for: the tray and small list views take
// 16 and 20, Explorer takes 32 and 48, and the large tiles take 128 and 256.
var sizes = []int{16, 20, 24, 32, 48, 64, 128, 256}

// iconMargin is the transparent border kept around the artwork, as a fraction
// of the icon. The mark is portrait, so squaring it for a square icon already
// leaves generous space either side of the pack -- adding a margin on top of
// that only makes it read small in the taskbar, hence zero.
const iconMargin = 0.0

func main() {
	src := "logo.png"
	out := filepath.Join("assets", "nomadwifi.ico")
	if len(os.Args) > 1 {
		src = os.Args[1]
	}
	if len(os.Args) > 2 {
		out = os.Args[2]
	}

	logo, err := loadTrimmed(src)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read logo:", err)
		os.Exit(1)
	}

	var images []*image.RGBA
	for _, s := range sizes {
		images = append(images, render(logo, s))
	}

	data, err := encodeICO(images)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d sizes, %d bytes)\n", out, len(sizes), len(data))

	// A large PNG is handy for the website and for READMEs.
	pngPath := filepath.Join(filepath.Dir(out), "nomadwifi-512.png")
	if f, err := os.Create(pngPath); err == nil {
		defer f.Close()
		if err := png.Encode(f, render(logo, 512)); err == nil {
			fmt.Printf("wrote %s\n", pngPath)
		}
	}
}

// loadTrimmed decodes the logo and crops it to the pixels that are actually
// painted. Without this the source PNG's own padding is baked in twice and the
// mark reads small in the tray.
func loadTrimmed(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	decoded, err := png.Decode(f)
	if err != nil {
		return nil, err
	}

	b := decoded.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := decoded.At(x, y).RGBA(); a > 0x2000 {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if minX > maxX || minY > maxY {
		return nil, fmt.Errorf("%s is fully transparent", path)
	}

	// Square the crop around the artwork so nothing is distorted later.
	w, h := maxX-minX+1, maxY-minY+1
	side := w
	if h > side {
		side = h
	}
	offX := minX - (side-w)/2
	offY := minY - (side-h)/2

	out := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			sx, sy := offX+x, offY+y
			if sx < b.Min.X || sx >= b.Max.X || sy < b.Min.Y || sy >= b.Max.Y {
				continue
			}
			r, g, bb, a := decoded.At(sx, sy).RGBA()
			out.SetRGBA(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bb >> 8), uint8(a >> 8)})
		}
	}
	return out, nil
}

// render scales the trimmed logo down to one icon size, leaving a small margin.
func render(logo *image.RGBA, size int) *image.RGBA {
	inner := int(float64(size) * (1 - 2*iconMargin))
	if inner < 1 {
		inner = size
	}
	scaled := resize(logo, inner)

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	off := (size - inner) / 2
	for y := 0; y < inner; y++ {
		for x := 0; x < inner; x++ {
			dst.SetRGBA(off+x, off+y, scaled.RGBAAt(x, y))
		}
	}
	return dst
}

// resize area-averages the source into a square of the given size. Averaging
// over the whole source footprint of each destination pixel is what keeps the
// mark clean at 16 pixels; sampling single pixels would alias badly.
func resize(src *image.RGBA, size int) *image.RGBA {
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	scale := float64(sb.Dx()) / float64(size)

	for y := 0; y < size; y++ {
		y0, y1 := spanFor(y, scale, sb.Dy())
		for x := 0; x < size; x++ {
			x0, x1 := spanFor(x, scale, sb.Dx())

			var r, g, b, a, n int
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					p := src.RGBAAt(sb.Min.X+sx, sb.Min.Y+sy)
					// Weight colour by alpha so transparent pixels do not
					// darken the edges.
					r += int(p.R) * int(p.A) / 255
					g += int(p.G) * int(p.A) / 255
					b += int(p.B) * int(p.A) / 255
					a += int(p.A)
					n++
				}
			}
			if n == 0 {
				continue
			}
			alpha := a / n
			if alpha == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: clamp8(r * 255 / n / max(alpha, 1)),
				G: clamp8(g * 255 / n / max(alpha, 1)),
				B: clamp8(b * 255 / n / max(alpha, 1)),
				A: clamp8(alpha),
			})
		}
	}
	return dst
}

// spanFor returns the half-open source range covered by destination index i.
func spanFor(i int, scale float64, limit int) (int, int) {
	lo := int(float64(i) * scale)
	hi := int(float64(i+1) * scale)
	if hi <= lo {
		hi = lo + 1
	}
	if hi > limit {
		hi = limit
	}
	if lo > limit-1 {
		lo = limit - 1
	}
	return lo, hi
}

func clamp8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// encodeICO packs the rendered sizes into a Windows .ico container.
//
// Sizes up to 128 are stored as uncompressed DIBs because GDI+, which backs
// the .NET tray icon, does not reliably decode PNG-compressed entries at
// small sizes. Only the 256 pixel entry uses PNG, which is where the size
// saving actually matters and where PNG support is universal.
func encodeICO(images []*image.RGBA) ([]byte, error) {
	type entry struct {
		width, height int
		payload       []byte
	}

	var entries []entry
	for _, img := range images {
		size := img.Bounds().Dx()

		var payload []byte
		if size >= 256 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				return nil, err
			}
			payload = buf.Bytes()
		} else {
			payload = encodeDIB(img)
		}
		entries = append(entries, entry{width: size, height: size, payload: payload})
	}

	var buf bytes.Buffer
	// ICONDIR
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // 1 = icon
	binary.Write(&buf, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		// A dimension of 256 is encoded as 0.
		dim := func(v int) uint8 {
			if v >= 256 {
				return 0
			}
			return uint8(v)
		}
		buf.WriteByte(dim(e.width))
		buf.WriteByte(dim(e.height))
		buf.WriteByte(0)                                    // palette size
		buf.WriteByte(0)                                    // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // colour planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(&buf, binary.LittleEndian, uint32(len(e.payload)))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(e.payload)
	}

	for _, e := range entries {
		buf.Write(e.payload)
	}
	return buf.Bytes(), nil
}

// encodeDIB writes a 32-bit bottom-up bitmap with the trailing AND mask that
// the icon format requires even when alpha carries the transparency.
func encodeDIB(img *image.RGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	var buf bytes.Buffer

	// BITMAPINFOHEADER. The height is doubled to cover the colour bitmap plus
	// the mask, which is what the icon format expects.
	binary.Write(&buf, binary.LittleEndian, uint32(40))
	binary.Write(&buf, binary.LittleEndian, int32(w))
	binary.Write(&buf, binary.LittleEndian, int32(h*2))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(32))
	binary.Write(&buf, binary.LittleEndian, uint32(0)) // BI_RGB
	binary.Write(&buf, binary.LittleEndian, uint32(w*h*4))
	for i := 0; i < 4; i++ {
		binary.Write(&buf, binary.LittleEndian, uint32(0))
	}

	// Colour data, bottom-up, BGRA.
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			p := img.RGBAAt(x, y)
			buf.WriteByte(p.B)
			buf.WriteByte(p.G)
			buf.WriteByte(p.R)
			buf.WriteByte(p.A)
		}
	}

	// AND mask: one bit per pixel, rows padded to 4 bytes. Fully transparent
	// pixels are masked out so the icon still looks right on the rare surface
	// that ignores the alpha channel.
	rowBytes := ((w + 31) / 32) * 4
	for y := h - 1; y >= 0; y-- {
		row := make([]byte, rowBytes)
		for x := 0; x < w; x++ {
			if img.RGBAAt(x, y).A < 128 {
				row[x/8] |= 0x80 >> (uint(x) % 8)
			}
		}
		buf.Write(row)
	}

	return buf.Bytes()
}
