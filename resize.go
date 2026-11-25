package docinfer

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"math"
)

const jpegQuality = 95

func fit(data []byte, maxPixels int) []byte {
	if maxPixels <= 0 {
		return data
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 {
		return data
	}
	if int64(cfg.Width)*int64(cfg.Height) <= int64(maxPixels) {
		return data
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	w, h := fitSize(cfg.Width, cfg.Height, maxPixels)
	dst := boxResize(src, w, h)
	var b bytes.Buffer
	switch format {
	case "png":
		err = png.Encode(&b, dst)
	case "jpeg":
		err = jpeg.Encode(&b, dst, &jpeg.Options{Quality: jpegQuality})
	}
	if err != nil {
		return data
	}
	_, gray := dst.(*image.Gray)
	if format == "png" {
		return carryPNG(data, b.Bytes(), gray)
	}
	return carryJPEG(data, b.Bytes(), gray)
}

func fitSize(w, h, maxPixels int) (int, int) {
	long, short := w, h
	if h > w {
		long, short = h, w
	}
	bound := int64(maxPixels)
	side := func(l int) int { return max(1, int(int64(l)*int64(short)/int64(long))) }
	l := int(float64(long) * math.Sqrt(float64(maxPixels)/(float64(w)*float64(h))))
	l = max(1, min(l, long))
	if side(l) == 1 {
		l = int(min(int64(long), bound))
	} else {
		for l > 1 && int64(l)*int64(side(l)) > bound {
			l--
		}
		for l < long && int64(l+1)*int64(side(l+1)) <= bound {
			l++
		}
	}
	if h > w {
		return side(l), l
	}
	return l, side(l)
}

type tap struct {
	i int
	w float64
}

func taps(src, dst int) [][]tap {
	scale := float64(src) / float64(dst)
	out := make([][]tap, dst)
	for j := range out {
		lo, hi := float64(j)*scale, float64(j+1)*scale
		for i := int(lo); i < src && float64(i) < hi; i++ {
			if w := min(hi, float64(i+1)) - max(lo, float64(i)); w > 0 {
				out[j] = append(out[j], tap{i, w / scale})
			}
		}
	}
	return out
}

func boxResize(src image.Image, w, h int) image.Image {
	b := src.Bounds()
	sw := b.Dx()
	gray, isGray := src.(*image.Gray)
	ch := 4
	if isGray {
		ch = 1
	}
	rgba64, isRGBA64 := src.(image.RGBA64Image)
	row := make([]float64, sw*ch)
	read := func(y int) {
		sy := b.Min.Y + y
		switch {
		case isGray:
			off := gray.PixOffset(b.Min.X, sy)
			for x := range sw {
				row[x] = float64(gray.Pix[off+x])
			}
		case isRGBA64:
			for x := range sw {
				c := rgba64.RGBA64At(b.Min.X+x, sy)
				row[4*x], row[4*x+1], row[4*x+2], row[4*x+3] = float64(c.R), float64(c.G), float64(c.B), float64(c.A)
			}
		default:
			for x := range sw {
				r, g, bl, a := src.At(b.Min.X+x, sy).RGBA()
				row[4*x], row[4*x+1], row[4*x+2], row[4*x+3] = float64(r), float64(g), float64(bl), float64(a)
			}
		}
	}

	xs, ys := taps(sw, w), taps(b.Dy(), h)
	acc := make([]float64, w*ch)
	var pix []uint8
	var stride int
	var dst image.Image
	if isGray {
		g := image.NewGray(image.Rect(0, 0, w, h))
		pix, stride, dst = g.Pix, g.Stride, g
	} else {
		r := image.NewRGBA(image.Rect(0, 0, w, h))
		pix, stride, dst = r.Pix, r.Stride, r
	}
	unit := 257.0
	if isGray {
		unit = 1
	}
	for dy, ty := range ys {
		clear(acc)
		for _, t := range ty {
			read(t.i)
			for dx, tx := range xs {
				for _, u := range tx {
					wt := t.w * u.w
					for c := range ch {
						acc[dx*ch+c] += wt * row[u.i*ch+c]
					}
				}
			}
		}
		out := pix[dy*stride:]
		for i, v := range acc {
			out[i] = uint8(min(255, math.Round(v/unit)))
		}
	}
	return dst
}
