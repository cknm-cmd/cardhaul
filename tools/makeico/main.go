// makeico writes a Windows .ico whose images are stored as 32-bit bitmaps (not PNG),
// which NSIS needs for installer icons. Usage: go run ./tools/makeico in.png out.ico
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	_ "image/png"
	"log"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: makeico in.png out.ico")
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	src, _, err := image.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	sizes := []int{16, 32, 48, 256}
	var images [][]byte
	for _, s := range sizes {
		images = append(images, dib(scale(src, s), s))
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, []uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := byte(s)
		if s == 256 {
			dim = 0 // 0 means 256 in the directory
		}
		out.Write([]byte{dim, dim, 0, 0})
		binary.Write(&out, binary.LittleEndian, []uint16{1, 32})
		binary.Write(&out, binary.LittleEndian, []uint32{uint32(len(images[i])), uint32(offset)})
		offset += len(images[i])
	}
	for _, img := range images {
		out.Write(img)
	}
	if err := os.WriteFile(os.Args[2], out.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}

// scale shrinks src to s×s by averaging the source pixels covered by each target pixel.
func scale(src image.Image, s int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, s, s))
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/s, b.Min.X+(x+1)*b.Dx()/s
			y0, y1 := b.Min.Y+y*b.Dy()/s, b.Min.Y+(y+1)*b.Dy()/s
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
					r += uint64(c.R) * uint64(c.A)
					g += uint64(c.G) * uint64(c.A)
					bl += uint64(c.B) * uint64(c.A)
					a += uint64(c.A)
					n++
				}
			}
			if a > 0 {
				dst.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(bl / a), uint8(a / n)})
			}
		}
	}
	return dst
}

// dib encodes img as a BITMAPINFOHEADER + bottom-up BGRA pixels + an empty AND mask.
func dib(img *image.NRGBA, s int) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, struct {
		Size                 uint32
		Width, Height        int32
		Planes, BitCount     uint16
		Compression, ImgSize uint32
		XPPM, YPPM           int32
		ClrUsed, ClrImp      uint32
	}{40, int32(s), int32(2 * s), 1, 32, 0, uint32(s * s * 4), 0, 0, 0, 0})
	for y := s - 1; y >= 0; y-- {
		for x := 0; x < s; x++ {
			c := img.NRGBAAt(x, y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	rowMask := ((s + 31) / 32) * 4
	buf.Write(make([]byte, rowMask*s))
	return buf.Bytes()
}
