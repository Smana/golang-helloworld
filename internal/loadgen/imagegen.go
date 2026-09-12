package loadgen

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"sync"
)

// lockedRand is a goroutine-safe, seedable random source.
type lockedRand struct {
	mu sync.Mutex
	r  *rand.Rand
}

func newLockedRand(seed int64) *lockedRand {
	// #nosec G404,G115 -- synthetic load-generator data (image pixels, sizes, IDs); sign
	// reinterpretation of a user-supplied seed is fine, not security sensitive
	return &lockedRand{r: rand.New(rand.NewPCG(uint64(seed), uint64(seed)^0x9e3779b97f4a7c15))}
}

func (l *lockedRand) IntN(n int) int { l.mu.Lock(); defer l.mu.Unlock(); return l.r.IntN(n) }
func (l *lockedRand) Float64() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Float64()
}

// generateImage returns an in-memory gradient of a random size (320-1920 px
// wide), as PNG or JPEG: varied sizes and formats exercise storage and the
// worker's decode and resize. Every generated file stays under the 10 MiB cap.
func generateImage(rnd *lockedRand) (data []byte, name, contentType string, err error) {
	w, h := 320+rnd.IntN(1601), 240+rnd.IntN(1201)
	// #nosec G115 -- Safe conversion: IntN(256) is always within 0-255
	from := color.RGBA{uint8(rnd.IntN(256)), uint8(rnd.IntN(256)), uint8(rnd.IntN(256)), 255}
	// #nosec G115 -- Safe conversion: IntN(256) is always within 0-255
	to := color.RGBA{uint8(rnd.IntN(256)), uint8(rnd.IntN(256)), uint8(rnd.IntN(256)), 255}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		t := float64(y) / float64(h)
		c := color.RGBA{lerp(from.R, to.R, t), lerp(from.G, to.G, t), lerp(from.B, to.B, t), 255}
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w*4; x += 4 {
			row[x], row[x+1], row[x+2], row[x+3] = c.R, c.G, c.B, c.A
		}
	}
	var buf bytes.Buffer
	ext := "png"
	contentType = "image/png"
	if rnd.IntN(2) == 0 {
		err = png.Encode(&buf, img)
	} else {
		ext, contentType = "jpg", "image/jpeg"
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	}
	return buf.Bytes(), fmt.Sprintf("loadgen-%d.%s", rnd.IntN(1_000_000_000), ext), contentType, err
}

func lerp(a, b uint8, t float64) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t) }
